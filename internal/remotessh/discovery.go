package remotessh

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	sshconfig "github.com/kevinburke/ssh_config"
)

const (
	defaultMaxConfigFiles = 128
	defaultMaxConfigBytes = 4 << 20
	maxConfigFileBytes    = 1 << 20
	maxIncludeDepth       = 5
)

var (
	ErrConfigDiscoveryBudget = errors.New("SSH config discovery exceeded its safety budget")
	ErrConfigDiscoveryParse  = errors.New("SSH config discovery could not parse configuration")
	ErrConfigDiscoverySource = errors.New("SSH config discovery could not read a configuration source")
)

// DiscoveryOptions controls the side-effect-free SSH config scanner. Relative
// Include paths are resolved against ConfigPath's directory. The scanner never
// invokes a shell, ssh, ssh -G, or any config command.
type DiscoveryOptions struct {
	ConfigPath       string
	HomeDir          string
	SystemConfigPath string
	MaxFiles         int
	MaxBytes         int64
}

type AliasRisk struct {
	HasMatchExec     bool
	HasProxyCommand  bool
	HasProxyJump     bool
	HasLocalCommand  bool
	HasRemoteCommand bool
	HasSetEnv        bool
}

type Alias struct {
	Name string
	Risk AliasRisk
}

type configBlock struct {
	patterns []string
	risk     AliasRisk
}

type configScanner struct {
	opts      DiscoveryOptions
	maxFiles  int
	maxBytes  int64
	bytesRead int64
	fileCount int
	seen      map[string]bool
	blocks    []configBlock
}

// DiscoverSSHConfig performs conservative static discovery of concrete Host
// aliases. Wildcard and negated patterns are not returned as connectable
// aliases, but they still contribute risk to concrete aliases that match them.
func DiscoverSSHConfig(opts DiscoveryOptions) ([]Alias, error) {
	if opts.HomeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("%w: determine home directory: %v", ErrConfigDiscoverySource, err)
		}
		opts.HomeDir = home
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = filepath.Join(opts.HomeDir, ".ssh", "config")
	}
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = defaultMaxConfigFiles
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = defaultMaxConfigBytes
	}
	if opts.MaxFiles > defaultMaxConfigFiles {
		opts.MaxFiles = defaultMaxConfigFiles
	}
	if opts.MaxBytes > defaultMaxConfigBytes {
		opts.MaxBytes = defaultMaxConfigBytes
	}

	scanner := &configScanner{
		opts:     opts,
		maxFiles: opts.MaxFiles,
		maxBytes: opts.MaxBytes,
		seen:     make(map[string]bool),
	}
	if err := scanner.loadRoot(opts.ConfigPath, filepath.Dir(opts.ConfigPath)); err != nil {
		return nil, err
	}
	if opts.SystemConfigPath != "" {
		if err := scanner.loadRoot(opts.SystemConfigPath, filepath.Dir(opts.SystemConfigPath)); err != nil {
			return nil, err
		}
	}

	return scanner.result(), nil
}

func (s *configScanner) loadRoot(path, includeBase string) error {
	path = filepath.Clean(path)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("%w: stat %q: %v", ErrConfigDiscoverySource, path, err)
	}
	return s.load(path, includeBase, 0)
}

func (s *configScanner) load(path, includeBase string, depth int) error {
	if depth > maxIncludeDepth {
		return fmt.Errorf("%w: Include depth exceeds %d", ErrConfigDiscoveryBudget, maxIncludeDepth)
	}
	canonical, err := canonicalSourcePath(path)
	if err != nil {
		return fmt.Errorf("%w: canonicalize %q: %v", ErrConfigDiscoverySource, path, err)
	}
	if s.seen[canonical] {
		return nil
	}
	if s.fileCount >= s.maxFiles {
		return fmt.Errorf("%w: more than %d files", ErrConfigDiscoveryBudget, s.maxFiles)
	}

	file, err := os.Open(canonical)
	if err != nil {
		return fmt.Errorf("%w: open %q: %v", ErrConfigDiscoverySource, canonical, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("%w: stat %q: %v", ErrConfigDiscoverySource, canonical, err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return fmt.Errorf("%w: source %q is not a regular file", ErrConfigDiscoverySource, canonical)
	}
	remaining := s.maxBytes - s.bytesRead
	if info.Size() > maxConfigFileBytes || info.Size() > remaining {
		_ = file.Close()
		return fmt.Errorf("%w: source %q is too large", ErrConfigDiscoveryBudget, canonical)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, min(int64(maxConfigFileBytes), remaining)+1))
	closeErr := file.Close()
	if readErr != nil {
		return fmt.Errorf("%w: read %q: %v", ErrConfigDiscoverySource, canonical, readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("%w: close %q: %v", ErrConfigDiscoverySource, canonical, closeErr)
	}
	if len(data) > maxConfigFileBytes || int64(len(data)) > remaining {
		return fmt.Errorf("%w: source %q is too large", ErrConfigDiscoveryBudget, canonical)
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("%w: source %q is not valid UTF-8", ErrConfigDiscoveryParse, canonical)
	}
	s.seen[canonical] = true
	s.fileCount++
	s.bytesRead += int64(len(data))

	includes, err := s.parseSource(canonical, string(data))
	if err != nil {
		return err
	}
	for _, include := range includes {
		matches, err := expandInclude(include.pattern, includeBase, s.opts.HomeDir)
		if err != nil {
			continue
		}
		if len(matches) == 0 {
			continue
		}
		for _, match := range matches {
			if err := s.load(match, includeBase, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

type includeDirective struct {
	pattern string
}

func (s *configScanner) parseSource(source, content string) ([]includeDirective, error) {
	var sanitized strings.Builder
	var includes []includeDirective
	hasMatchExec := false
	for lineNumber, line := range strings.Split(content, "\n") {
		words, err := configWords(line)
		if err != nil {
			return nil, fmt.Errorf("%w: %s:%d: %v", ErrConfigDiscoveryParse, source, lineNumber+1, err)
		}
		words, keyword, err := normalizeDirectiveWords(words)
		if err != nil {
			return nil, fmt.Errorf("%w: %s:%d: %v", ErrConfigDiscoveryParse, source, lineNumber+1, err)
		}
		switch keyword {
		case "include":
			if len(words) < 2 {
				return nil, fmt.Errorf("%w: %s:%d: Include requires a path", ErrConfigDiscoveryParse, source, lineNumber+1)
			}
			for _, pattern := range words[1:] {
				includes = append(includes, includeDirective{pattern: pattern})
			}
			sanitized.WriteString("# Include resolved by Pi Desk static discovery\n")
		case "match":
			for _, criterion := range words[1:] {
				if strings.EqualFold(criterion, "exec") {
					hasMatchExec = true
					break
				}
			}
			// Match evaluation can depend on connection-time state. Treat all
			// directives in the block as globally risky during static browse.
			sanitized.WriteString("Match all\n")
		default:
			sanitized.WriteString(line)
			sanitized.WriteByte('\n')
		}
	}

	config, err := sshconfig.Decode(strings.NewReader(sanitized.String()))
	if err != nil {
		return nil, fmt.Errorf("%w: parse %q: %v", ErrConfigDiscoveryParse, source, err)
	}
	if hasMatchExec {
		s.blocks = append(s.blocks, configBlock{
			patterns: []string{"*"},
			risk:     AliasRisk{HasMatchExec: true},
		})
	}
	for _, host := range config.Hosts {
		block := configBlock{}
		for _, pattern := range host.Patterns {
			block.patterns = append(block.patterns, pattern.String())
		}
		for _, node := range host.Nodes {
			keyValue, ok := node.(*sshconfig.KV)
			if !ok {
				continue
			}
			block.risk = applyRisk(block.risk, strings.ToLower(keyValue.Key), []string{keyValue.Value})
		}
		if len(block.patterns) > 0 {
			s.blocks = append(s.blocks, block)
		}
	}
	return includes, nil
}

func normalizeDirectiveWords(words []string) ([]string, string, error) {
	if len(words) == 0 {
		return words, "", nil
	}
	keyword := strings.ToLower(words[0])
	if index := strings.IndexByte(keyword, '='); index >= 0 {
		if index == len(keyword)-1 {
			return nil, "", errors.New("empty directive value")
		}
		words = append([]string{keyword[:index], words[0][index+1:]}, words[1:]...)
		keyword = words[0]
	}
	if len(words) >= 2 && words[1] == "=" {
		words = append(words[:1], words[2:]...)
	}
	return words, keyword, nil
}

func applyRisk(risk AliasRisk, keyword string, values []string) AliasRisk {
	configured := func() bool {
		if len(values) == 0 {
			return true
		}
		value := strings.ToLower(strings.TrimSpace(strings.Join(values, " ")))
		return value != "" && value != "none" && value != "no" && value != "false"
	}
	switch keyword {
	case "proxycommand":
		risk.HasProxyCommand = configured()
	case "proxyjump":
		risk.HasProxyJump = configured()
	case "localcommand":
		risk.HasLocalCommand = configured()
	case "remotecommand":
		risk.HasRemoteCommand = configured()
	case "setenv":
		risk.HasSetEnv = configured()
	}
	return risk
}

func (s *configScanner) result() []Alias {
	aliasNames := make([]string, 0)
	seenNames := make(map[string]bool)
	for _, block := range s.blocks {
		for _, pattern := range block.patterns {
			if !isConcreteAlias(pattern) {
				continue
			}
			if !seenNames[pattern] {
				seenNames[pattern] = true
				aliasNames = append(aliasNames, pattern)
			}
		}
	}

	aliases := make([]Alias, 0, len(aliasNames))
	for _, name := range aliasNames {
		var risk AliasRisk
		for _, block := range s.blocks {
			if sshPatternsMatch(block.patterns, name) {
				risk = mergeRisk(risk, block.risk)
			}
		}
		aliases = append(aliases, Alias{Name: name, Risk: risk})
	}
	return aliases
}

func mergeRisk(left, right AliasRisk) AliasRisk {
	return AliasRisk{
		HasMatchExec:     left.HasMatchExec || right.HasMatchExec,
		HasProxyCommand:  left.HasProxyCommand || right.HasProxyCommand,
		HasProxyJump:     left.HasProxyJump || right.HasProxyJump,
		HasLocalCommand:  left.HasLocalCommand || right.HasLocalCommand,
		HasRemoteCommand: left.HasRemoteCommand || right.HasRemoteCommand,
		HasSetEnv:        left.HasSetEnv || right.HasSetEnv,
	}
}

func canonicalSourcePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	return filepath.Clean(absolute), nil
}

func expandInclude(pattern, baseDir, homeDir string) ([]string, error) {
	if pattern == "" || strings.ContainsRune(pattern, '\x00') {
		return nil, errors.New("invalid Include path")
	}
	if strings.ContainsAny(pattern, "%") {
		return nil, errors.New("Include token expansion is not available during static discovery")
	}
	if pattern == "~" {
		pattern = homeDir
	} else if strings.HasPrefix(pattern, "~/") || strings.HasPrefix(pattern, `~\`) {
		pattern = filepath.Join(homeDir, pattern[2:])
	} else if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(baseDir, pattern)
	}
	matches, err := filepath.Glob(filepath.Clean(pattern))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

func configWords(line string) ([]string, error) {
	line = strings.TrimSuffix(line, "\r")
	words := make([]string, 0, 4)
	var word strings.Builder
	var quote byte
	escaped := false
	flush := func() {
		if word.Len() > 0 {
			words = append(words, word.String())
			word.Reset()
		}
	}
	for index := 0; index < len(line); index++ {
		value := line[index]
		if escaped {
			word.WriteByte(value)
			escaped = false
			continue
		}
		if value == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if value == quote {
				quote = 0
			} else {
				word.WriteByte(value)
			}
			continue
		}
		switch value {
		case '\'', '"':
			quote = value
		case '#':
			if index == 0 || unicode.IsSpace(rune(line[index-1])) {
				flush()
				index = len(line)
			}
		case ' ', '\t':
			flush()
		default:
			word.WriteByte(value)
		}
	}
	if escaped {
		word.WriteByte('\\')
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	flush()
	return words, nil
}

func sshPatternsMatch(patterns []string, value string) bool {
	parsed := make([]*sshconfig.Pattern, 0, len(patterns))
	for _, pattern := range patterns {
		parsedPattern, err := sshconfig.NewPattern(pattern)
		if err != nil {
			return false
		}
		parsed = append(parsed, parsedPattern)
	}
	return (&sshconfig.Host{Patterns: parsed}).Matches(value)
}

func isConcreteAlias(pattern string) bool {
	return pattern != "" && !strings.HasPrefix(pattern, "!") && !strings.ContainsAny(pattern, "*?")
}
