package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// tagCommit returns the commit to record for tag when --commit was not given:
// the commit the work tree above dir is checked out at. Where the metadata
// allows it without starting git, the tag is resolved too and must point at
// that same commit, so a work tree that sits on another commit is rejected
// instead of being recorded under the wrong tag.
func tagCommit(dir, tag string) (string, error) {
	gitDir, err := findGitDir(dir)
	if err != nil {
		return "", fmt.Errorf("no Git metadata above %s (%v): pass --commit with the commit of %s", dir, err, tag)
	}
	refs := commonDir(gitDir)
	head, err := headCommit(gitDir, refs)
	if err != nil {
		return "", fmt.Errorf("HEAD of the repository above %s cannot be read (%v): pass --commit with the commit of %s", dir, err, tag)
	}
	tagged, known, err := peelTag(refs, tag)
	if err != nil {
		return "", fmt.Errorf("%s cannot be resolved in the repository above %s (%v): fetch the tag, or pass --commit", tag, dir, err)
	}
	if known && tagged != head {
		return "", fmt.Errorf("the work tree above %s is at %s but %s is %s: check out the tag, or export it with git archive and pass --commit",
			dir, head[:7], tag, tagged[:7])
	}
	return head, nil
}

// findGitDir walks up from dir to the repository's .git directory. A .git
// file (linked work tree, submodule) is followed through its gitdir line.
func findGitDir(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, ".git")
		info, err := os.Stat(candidate)
		switch {
		case err == nil && info.IsDir():
			return candidate, nil
		case err == nil:
			data, err := os.ReadFile(candidate)
			if err != nil {
				return "", err
			}
			target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
			if !ok {
				return "", fmt.Errorf("%s is not a gitdir file", candidate)
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(dir, target)
			}
			return target, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fs.ErrNotExist
		}
		dir = parent
	}
}

// commonDir returns the directory that holds refs and objects: gitDir itself,
// or the main repository's directory for a linked work tree.
func commonDir(gitDir string) string {
	data, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return gitDir
	}
	common := strings.TrimSpace(string(data))
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitDir, common)
	}
	return common
}

// headCommit resolves HEAD: a detached object name, or a branch.
func headCommit(gitDir, refs string) (string, error) {
	data, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", err
	}
	head := strings.TrimSpace(string(data))
	if name, ok := strings.CutPrefix(head, "ref: "); ok {
		id, _, _, err := readRef(refs, name)
		if err != nil {
			return "", err
		}
		head = id
	}
	if !isObjectID(head) {
		return "", fmt.Errorf("HEAD does not resolve to a commit")
	}
	return head, nil
}

// readRef returns the object a ref points at. For a ref found in packed-refs
// it also returns the peeled object when Git recorded one, and whether the
// file declares itself fully peeled (then a ref without a peeled line is known
// not to be an annotated tag).
func readRef(refs, name string) (id, peeled string, fullyPeeled bool, err error) {
	loose, err := os.ReadFile(filepath.Join(refs, filepath.FromSlash(name)))
	if err == nil {
		return strings.TrimSpace(string(loose)), "", false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", "", false, err
	}
	packed, err := os.Open(filepath.Join(refs, "packed-refs"))
	if err != nil {
		return "", "", false, fmt.Errorf("ref %s: %w", name, fs.ErrNotExist)
	}
	defer packed.Close()
	scanner := bufio.NewScanner(packed)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "#"):
			fullyPeeled = fullyPeeled || strings.Contains(line, "fully-peeled")
		case id != "":
			// The line after the ref: its peeled object, or the next ref.
			return id, peeledOf(line), fullyPeeled, nil
		default:
			if object, ref, ok := strings.Cut(line, " "); ok && ref == name {
				id = object
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", false, err
	}
	if id == "" {
		return "", "", false, fmt.Errorf("ref %s: %w", name, fs.ErrNotExist)
	}
	return id, "", fullyPeeled, nil
}

// peeledOf returns the object of a "^object" line and "" for any other line.
func peeledOf(line string) string {
	if object, ok := strings.CutPrefix(line, "^"); ok {
		return object
	}
	return ""
}

// peelTag returns the commit a tag points at. known is false when that cannot
// be decided without reading pack files (an annotated tag whose object is
// packed and has no peeled line), which is not an error.
func peelTag(refs, tag string) (commit string, known bool, err error) {
	id, peeled, fullyPeeled, err := readRef(refs, "refs/tags/"+tag)
	if err != nil {
		return "", false, err
	}
	if peeled != "" {
		return peeled, true, nil
	}
	if fullyPeeled {
		return id, true, nil
	}
	for depth := 0; depth < 5; depth++ {
		kind, target, err := looseObject(refs, id)
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		switch kind {
		case "commit":
			return id, true, nil
		case "tag":
			id = target
		default:
			return "", false, fmt.Errorf("tag %s points at a %s", tag, kind)
		}
	}
	return "", false, fmt.Errorf("tag %s is nested too deeply", tag)
}

// looseObject reads the type of a loose object and, for a tag object, the
// object it names.
func looseObject(refs, id string) (kind, target string, err error) {
	if !isObjectID(id) {
		return "", "", fmt.Errorf("%q is not an object name", id)
	}
	file, err := os.Open(filepath.Join(refs, "objects", id[:2], id[2:]))
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	reader, err := zlib.NewReader(file)
	if err != nil {
		return "", "", err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, 1<<16))
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", "", err
	}
	header, body, ok := bytes.Cut(data, []byte{0})
	if !ok {
		return "", "", fmt.Errorf("object %s has no header", id)
	}
	kind, _, _ = strings.Cut(string(header), " ")
	if kind == "tag" {
		for _, line := range strings.Split(string(body), "\n") {
			if object, ok := strings.CutPrefix(line, "object "); ok {
				return kind, object, nil
			}
		}
		return "", "", fmt.Errorf("tag object %s names no object", id)
	}
	return kind, "", nil
}
