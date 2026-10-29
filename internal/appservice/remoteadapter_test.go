package appservice

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRemoteAdapterManifestPinsContentCompatibilityAndCoverage(t *testing.T) {
	manifest, err := verifyRemoteAdapterBundle("0.84.2")
	if err != nil {
		t.Fatal(err)
	}
	for _, compatible := range []string{"0.85.0", "0.87.0", "999.0.0"} {
		if _, err := verifyRemoteAdapterBundle(compatible); err != nil {
			t.Fatalf("compatible Pi version rejected: %q (%v)", compatible, err)
		}
	}
	if manifest.Protocol != remoteAdapterProtocol || !slices.Equal(manifest.Coverage, remoteAdapterCoverage) {
		t.Fatalf("manifest=%#v", manifest)
	}
	for _, incompatible := range []string{"", "0.84.1", "invalid"} {
		if _, err := verifyRemoteAdapterBundle(incompatible); err == nil {
			t.Fatalf("incompatible Pi version accepted: %q", incompatible)
		}
	}
	if validRemoteAdapterHandshake(manifest, "wrong", manifest.Coverage) || validRemoteAdapterHandshake(manifest, manifest.Protocol, manifest.Coverage[:len(manifest.Coverage)-1]) {
		t.Fatal("incomplete adapter coverage handshake was accepted")
	}
	if !validRemoteAdapterHandshake(manifest, manifest.Protocol, manifest.Coverage) {
		t.Fatal("exact adapter coverage handshake was rejected")
	}
	broker := &remoteTaskBroker{manifest: manifest, handshakeDone: make(chan struct{}), ctx: context.Background()}
	if _, err := broker.execute(context.Background(), remoteBrokerRequest{Operation: "hello", Protocol: "wrong", Coverage: manifest.Coverage}, 123); err == nil {
		t.Fatal("invalid broker handshake succeeded")
	}
	if err := broker.waitHandshake(context.Background(), 123); err == nil {
		t.Fatal("invalid broker handshake did not fail startup admission")
	}
	closed := make(chan struct{})
	close(closed)
	peerFenced := &remoteTaskBroker{ctx: context.Background(), handshakeDone: closed, launcherPID: 2147483646}
	if _, err := peerFenced.execute(context.Background(), remoteBrokerRequest{Operation: "stat", Path: "."}, 2147483647); err == nil {
		t.Fatal("non-Pi broker peer was accepted after handshake")
	}

	path := filepath.Join(t.TempDir(), "adapter.ts")
	if err := os.WriteFile(path, remoteAdapterSource, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyRemoteAdapterFile(path, manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(append([]byte(nil), remoteAdapterSource...), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyRemoteAdapterFile(path, manifest); err == nil {
		t.Fatal("modified adapter file was accepted")
	}
}

func TestRemoteAdapterRoutesSkillsAndReportsCommandResults(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is unavailable")
	}
	// Keep the actual routing and filesystem checks; replace only external SDK imports.
	lines := strings.Split(string(remoteAdapterSource), "\n")
	for index, line := range lines {
		if strings.Contains(line, `from "@earendil-works/pi-coding-agent"`) || strings.Contains(line, `from "typebox"`) {
			lines[index] = ""
		}
	}
	directory := t.TempDir()
	source := "const createReadTool = globalThis.nativeReadFactory; const truncateTail = globalThis.nativeTruncateTail; const Type = new Proxy({}, {get: () => () => ({})});\n" + strings.Join(lines, "\n")
	// Node's strip-only mode needs the equivalent explicit parameter assignment.
	source = strings.ReplaceAll(source, "constructor(readonly code: string, message: string) { super(`${code}: ${message}`); }", "constructor(code: string, message: string) { super(`${code}: ${message}`); this.code = code; }")
	if err := os.WriteFile(filepath.Join(directory, "adapter.ts"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `import assert from 'node:assert/strict';
import * as fs from 'node:fs/promises';
import net from 'node:net';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
const directory = path.dirname(fileURLToPath(import.meta.url));
const skill = path.join(directory, 'skill'), outside = path.join(directory, 'skill-sibling');
await fs.mkdir(path.join(skill, 'references'), {recursive:true});
await fs.mkdir(outside);
await fs.writeFile(path.join(skill, 'SKILL.md'), 'Local skill instructions');
await fs.writeFile(path.join(skill, 'references', 'guide.md'), 'Reference content');
await fs.writeFile(path.join(outside, 'secret.txt'), 'Outside the skill');
await fs.symlink(outside, path.join(skill, 'escape'), process.platform === 'win32' ? 'junction' : 'dir');
const reads = [], requests = [];
globalThis.nativeReadFactory = () => ({execute: async (id, params, signal, onUpdate) => {
 reads.push({id, params, signal, onUpdate});
 return {content:[{type:'text', text:await fs.readFile(params.path, 'utf8')}]};
}});
globalThis.nativeTruncateTail = output => ({content:output === 'large output' ? 'kept tail' : output, truncated:output === 'large output'});
const socket = process.platform === 'win32' ? '\\\\.\\pipe\\pi-desk-local-skill-test-' + process.pid : path.join(directory, 'broker.sock');
const server = net.createServer(connection => {
 let buffer = Buffer.alloc(0);
 connection.on('data', chunk => {
  buffer = Buffer.concat([buffer, chunk]);
  if (buffer.length < 4 || buffer.length < buffer.readUInt32BE(0) + 4) return;
  const request = JSON.parse(buffer.subarray(4).toString('utf8'));
  requests.push(request);
  const result = request.operation === 'bash' ? {
   output:request.content.includes('large') ? 'large output' : request.content === 'empty-failure' ? '' : 'Remote command',
   exitCode:request.content.includes('failure') ? 7 : 0,
  } : {content:'Remote file: ' + request.path};
  const response = Buffer.from(JSON.stringify({id:request.id, ok:true, result}));
  const header = Buffer.alloc(4); header.writeUInt32BE(response.length);
  connection.end(Buffer.concat([header, response]));
 });
});
await new Promise((resolve, reject) => {server.once('error', reject); server.listen(socket, resolve);});
try {
 process.env.PI_DESK_REMOTE_SOCKET = socket;
 process.env.PI_DESK_REMOTE_TOKEN = 'test-token';
 process.env.PI_DESK_REMOTE_ROOT = '/remote-project';
 const {default:extension} = await import('./adapter.ts');
 let commands = [{source:'skill', sourceInfo:{path:path.join(skill, 'SKILL.md')}}];
 const toolsFor = enabled => {
  process.env.PI_DESK_REMOTE_LOCAL_SKILLS = String(enabled);
  const tools = {};
  extension({on(){}, getCommands:()=>commands, registerTool:tool=>tools[tool.name]=tool});
  return tools;
 };
 const tools = toolsFor(true), signal = new AbortController().signal, update = () => {};
 const read = file => tools.read.execute('read', {path:file}, signal, update);
 assert.equal((await read(path.join(skill, 'SKILL.md'))).content[0].text, 'Local skill instructions');
 assert.equal((await read(path.join(skill, 'references', 'guide.md'))).content[0].text, 'Reference content');
 await tools.read.execute('paged', {path:path.join(skill, 'SKILL.md'), offset:2, limit:3}, signal, update);
 assert.equal(reads.at(-1).params.offset, 2); assert.equal(reads.at(-1).params.limit, 3);
 assert.equal(reads.at(-1).signal, signal); assert.equal(reads.at(-1).onUpdate, update);
 assert.equal(requests.length, 0);
 await assert.rejects(read(path.join(outside, 'secret.txt')));
 await assert.rejects(read(path.join(skill, '..', 'skill-sibling', 'secret.txt')));
 await assert.rejects(read(path.join(skill, 'escape', 'secret.txt')), /outside the loaded skill directory/);
 await assert.rejects(tools.write.execute('write', {path:path.join(skill, 'SKILL.md'), content:'overwrite'}));
 assert.equal(await fs.readFile(path.join(skill, 'SKILL.md'), 'utf8'), 'Local skill instructions');
 commands = []; await assert.rejects(read(path.join(skill, 'SKILL.md')));
 commands = [{source:'extension', sourceInfo:{path:path.join(skill, 'SKILL.md')}}];
 await assert.rejects(read(path.join(skill, 'SKILL.md')));
 commands = [{source:'skill', path:path.join(skill, 'SKILL.md')}];
 assert.equal((await read(path.join(skill, 'SKILL.md'))).content[0].text, 'Local skill instructions');
 const before = reads.length;
 const disabled = toolsFor(false);
 await assert.rejects(disabled.read.execute('read', {path:path.join(skill, 'SKILL.md')}));
 assert.equal(reads.length, before);
 assert.equal((await read('/remote-project/main.go')).content[0].text, 'Remote file: main.go');
 assert.equal((await disabled.read.execute('read', {path:'README.md'})).content[0].text, 'Remote file: README.md');
 await tools.bash.execute('bash', {command:'echo remote'});
 assert.equal(requests.at(-1).operation, 'bash'); assert.equal(requests.at(-1).content, 'echo remote');
 for (const command of ['success', 'failure', 'empty-failure', 'large', 'large-failure']) {
  const updates = [];
  const result = await tools.bash.execute('bash', {command}, undefined, update => updates.push(update));
  const text = result.content[0].text;
  assert.equal(result.isError, command.includes('failure'));
  if (result.isError) assert.match(text, /Command exited with code 7$/);
  if (command.includes('large')) {assert.match(text, /^kept tail/); assert.match(text, /Output truncated/); assert.doesNotMatch(text, /large output/);}
  if (command === 'empty-failure') assert.match(text, /no output/);
  assert.deepEqual(updates.at(-1).content, result.content);
 }
 console.log('Local skill boundaries, remote routing, exit status, and truncated output passed');
} finally {await new Promise(resolve => server.close(resolve));}
`
	if err := os.WriteFile(filepath.Join(directory, "test.mjs"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, node, "--experimental-strip-types", filepath.Join(directory, "test.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("local skill routing: %v\n%s", err, output)
	}
}
