package workspace

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

type TargetRegistration struct {
	Name              string
	HostAlias         string
	ConfigFingerprint string
	HostKeyAlgorithm  string
	HostKeySHA256     string
}

type SSHWorkspaceRegistration struct {
	WorkspaceID   string
	Name          string
	TargetID      string
	RequestedRoot string
	CanonicalRoot string
	Device        uint64
	Inode         uint64
	RemoteOS      string
	RemoteArch    string
	Trust         string
}

func (catalog *Catalog) ListTargets() ([]TargetRecord, error) {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return nil, err
	}
	return slices.Clone(catalog.targets), nil
}

func (catalog *Catalog) ResolveTarget(id string) (TargetRecord, error) {
	id = strings.TrimSpace(id)
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return TargetRecord{}, err
	}
	for _, target := range catalog.targets {
		if target.ID == id {
			return target, nil
		}
	}
	return TargetRecord{}, errors.New("SSH target not found")
}

// RegisterTarget creates a random immutable target identity or refreshes only
// a byte-for-byte matching alias binding. Identity drift is never accepted by
// this method and does not mutate persisted state.
func (catalog *Catalog) RegisterTarget(registration TargetRegistration) (TargetRecord, error) {
	binding := HostKeyBinding{
		Algorithm:         strings.TrimSpace(registration.HostKeyAlgorithm),
		SHA256:            strings.TrimSpace(registration.HostKeySHA256),
		ConfigFingerprint: strings.TrimSpace(registration.ConfigFingerprint),
	}
	candidate := TargetRecord{
		Name:      strings.TrimSpace(registration.Name),
		HostAlias: strings.TrimSpace(registration.HostAlias),
		HostKey:   binding,
	}
	if err := validateHostAlias(candidate.HostAlias); err != nil {
		return TargetRecord{}, err
	}
	if !validDisplayName(candidate.Name) {
		return TargetRecord{}, errors.New("target name is invalid")
	}
	if err := validateHostKeyBinding(binding); err != nil {
		return TargetRecord{}, err
	}

	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return TargetRecord{}, err
	}
	now := catalog.now().UTC()
	targets := slices.Clone(catalog.targets)
	for index := range targets {
		if targetAliasKey(targets[index].HostAlias) != targetAliasKey(candidate.HostAlias) {
			continue
		}
		if targets[index].HostKey != candidate.HostKey {
			return TargetRecord{}, ErrTargetIdentityChanged
		}
		targets[index].Name = candidate.Name
		targets[index].LastConnectedAt = now
		if err := catalog.saveStateLocked(catalog.records, targets, catalog.desktop); err != nil {
			return TargetRecord{}, err
		}
		catalog.targets = targets
		return targets[index], nil
	}
	id, err := newIdentity("target")
	if err != nil {
		return TargetRecord{}, err
	}
	candidate.ID = id
	candidate.AddedAt = now
	candidate.LastConnectedAt = now
	if err := validateTarget(candidate); err != nil {
		return TargetRecord{}, err
	}
	targets = append(targets, candidate)
	if err := catalog.saveStateLocked(catalog.records, targets, catalog.desktop); err != nil {
		return TargetRecord{}, err
	}
	catalog.targets = targets
	return candidate, nil
}

func (catalog *Catalog) Rename(id, name string) (Record, error) {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if id == "" {
		return Record{}, errors.New("workspace id is required")
	}
	if !validDisplayName(name) {
		return Record{}, errors.New("workspace name is invalid")
	}

	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return Record{}, err
	}

	records := slices.Clone(catalog.records)
	for index := range records {
		if records[index].ID != id {
			continue
		}
		records[index].Name = name
		if err := validateRecord(records[index]); err != nil {
			return Record{}, err
		}
		if err := catalog.saveLocked(records, catalog.desktop); err != nil {
			return Record{}, err
		}
		catalog.records = records
		return records[index], nil
	}

	return Record{}, errors.New("workspace not found")
}

func (catalog *Catalog) AddSSHWorkspace(registration SSHWorkspaceRegistration) (Record, error) {
	return catalog.AddSSHWorkspaceAfter(registration, nil)
}

func (catalog *Catalog) FindSSHWorkspaceByRemoteIdentity(targetID, canonicalRoot string, device, inode uint64) (Record, bool, error) {
	targetID, canonicalRoot = strings.TrimSpace(targetID), strings.TrimSpace(canonicalRoot)
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return Record{}, false, err
	}
	for _, record := range catalog.records {
		ssh := record.Location.SSH
		if record.Location.Kind == KindSSH && ssh.TargetID == targetID && ssh.CanonicalRoot == canonicalRoot && ssh.Device == device && ssh.Inode == inode {
			return record, true, nil
		}
	}
	return Record{}, false, nil
}

func (catalog *Catalog) FindSSHWorkspaceByRequestedRoot(targetID, requestedRoot string) (Record, bool, error) {
	targetID, requestedRoot = strings.TrimSpace(targetID), strings.TrimSpace(requestedRoot)
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return Record{}, false, err
	}
	for _, record := range catalog.records {
		ssh := record.Location.SSH
		if record.Location.Kind == KindSSH && ssh.TargetID == targetID && ssh.RequestedRoot == requestedRoot {
			return record, true, nil
		}
	}
	return Record{}, false, nil
}

// AddSSHWorkspaceAfter invokes beforeDeny after validation and before a deny
// decision is persisted, allowing runtime capability revocation to happen first.
func (catalog *Catalog) AddSSHWorkspaceAfter(registration SSHWorkspaceRegistration, beforeDeny func(targetID string) error) (Record, error) {
	if registration.Trust != "approve" && registration.Trust != "deny" {
		return Record{}, errors.New("workspace trust must be approve or deny")
	}
	name := strings.TrimSpace(registration.Name)
	if !validDisplayName(name) {
		return Record{}, errors.New("workspace name is invalid")
	}

	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return Record{}, err
	}
	targetIndex := slices.IndexFunc(catalog.targets, func(target TargetRecord) bool { return target.ID == registration.TargetID })
	if targetIndex < 0 {
		return Record{}, errors.New("SSH target is not registered")
	}
	target := catalog.targets[targetIndex]
	location := SSHLocation{
		TargetID: registration.TargetID, RequestedRoot: registration.RequestedRoot,
		CanonicalRoot: registration.CanonicalRoot, Device: registration.Device, Inode: registration.Inode,
		RemoteOS: registration.RemoteOS, RemoteArch: registration.RemoteArch, HostKeyBinding: target.HostKey,
	}
	if err := validateSSHLocation(location); err != nil {
		return Record{}, err
	}
	now := catalog.now().UTC()
	records := slices.Clone(catalog.records)
	for index := range records {
		ssh := records[index].Location.SSH
		if records[index].Location.Kind != KindSSH || ssh.TargetID != location.TargetID || ssh.CanonicalRoot != location.CanonicalRoot || ssh.Device != location.Device || ssh.Inode != location.Inode {
			continue
		}
		// A root is a stable workspace identity. Reusing it with a new
		// one-shot candidate WorkspaceID is idempotent; only a changed host
		// binding or remote platform is an identity drift.
		if ssh.HostKeyBinding != location.HostKeyBinding || ssh.RemoteOS != location.RemoteOS || ssh.RemoteArch != location.RemoteArch {
			return Record{}, ErrTargetIdentityChanged
		}
		if registration.Trust == "deny" && beforeDeny != nil {
			if err := beforeDeny(registration.TargetID); err != nil {
				return Record{}, err
			}
		}
		records[index].Name = name
		records[index].Trust = registration.Trust
		records[index].LastOpenedAt = now
		if err := catalog.saveLocked(records, catalog.desktop); err != nil {
			return Record{}, err
		}
		catalog.records = records
		return records[index], nil
	}
	id := strings.TrimSpace(registration.WorkspaceID)
	if id == "" {
		var err error
		id, err = newIdentity("workspace")
		if err != nil {
			return Record{}, err
		}
	} else if !validIdentity("workspace", id) {
		return Record{}, errors.New("workspace identity is invalid")
	}
	record := Record{
		ID: id, Name: name, Location: Location{Kind: KindSSH, SSH: location}, Trust: registration.Trust,
		AddedAt: now, LastOpenedAt: now,
	}
	if err := validateRecord(record); err != nil {
		return Record{}, err
	}
	if registration.Trust == "deny" && beforeDeny != nil {
		if err := beforeDeny(registration.TargetID); err != nil {
			return Record{}, err
		}
	}
	records = append(records, record)
	if err := catalog.saveLocked(records, catalog.desktop); err != nil {
		return Record{}, err
	}
	catalog.records = records
	return record, nil
}

func (catalog *Catalog) RemoveTarget(id string) error {
	return catalog.RemoveTargetAfter(id, nil)
}

// RemoveTargetAfter invokes beforeRemove only after all references and target
// identity have been validated, but before state is persisted.
func (catalog *Catalog) RemoveTargetAfter(id string, beforeRemove func() error) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("target id is required")
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if err := catalog.loadLocked(); err != nil {
		return err
	}
	if slices.ContainsFunc(catalog.records, func(record Record) bool {
		return record.Location.Kind == KindSSH && record.Location.SSH.TargetID == id
	}) {
		return errors.New("target is still referenced by a workspace")
	}
	targets := slices.Clone(catalog.targets)
	index := slices.IndexFunc(targets, func(target TargetRecord) bool { return target.ID == id })
	if index < 0 {
		return errors.New("target not found")
	}
	targets = slices.Delete(targets, index, index+1)
	if beforeRemove != nil {
		if err := beforeRemove(); err != nil {
			return err
		}
	}
	if err := catalog.saveStateLocked(catalog.records, targets, catalog.desktop); err != nil {
		return fmt.Errorf("remove SSH target: %w", err)
	}
	catalog.targets = targets
	return nil
}
