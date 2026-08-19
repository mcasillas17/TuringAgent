// Package skillfiles reads the user's local SKILL.md library without caching
// file-owned metadata in SQLite.
package skillfiles

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

const (
	maxSkillFileBytes      = 256 * 1024
	maxReferenceFileBytes  = 256 * 1024
	maxReferenceTotalBytes = 1024 * 1024
	maxReferenceFiles      = 64
	maxNameRunes           = 120
	maxDescriptionRunes    = 500
	maxCapabilityRunes     = 128
)

var capabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*\.[a-z][a-z0-9_.-]*$`)

type Skill struct {
	ID          string
	Name        string
	Description string
	Category    string
	Body        string
	Version     string
	Author      string
	License     string
	Requires    []string
	References  map[string]string
	ParseError  string
	FolderPath  string
	Revision    string
}

type Store struct {
	root string
}

func New(root string) *Store {
	return &Store{root: filepath.Clean(root)}
}

func (s *Store) Root() string {
	return s.root
}

func (s *Store) Scan() ([]Skill, error) {
	info, err := os.Lstat(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect skills root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("skills root must be a real directory")
	}

	var skills []Skill
	err = filepath.WalkDir(s.root, func(skillFile string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if skillFile == s.root {
				return walkErr
			}
			relative, relErr := filepath.Rel(s.root, skillFile)
			if relErr != nil {
				return walkErr
			}
			id := filepath.ToSlash(relative)
			skills = append(skills, Skill{
				ID:         id,
				Name:       filepath.Base(skillFile),
				Category:   filepath.ToSlash(filepath.Dir(relative)),
				FolderPath: skillFile,
				ParseError: fmt.Sprintf("scan path: %v", walkErr),
			})
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Name() != "SKILL.md" {
			return nil
		}
		folder := filepath.Dir(skillFile)
		id, identityErr := s.skillID(folder)
		skill := Skill{
			ID:         id,
			Name:       filepath.Base(folder),
			Category:   filepath.ToSlash(filepath.Dir(filepath.FromSlash(id))),
			FolderPath: folder,
		}
		if identityErr != nil {
			skill.ParseError = identityErr.Error()
			skills = append(skills, skill)
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			skill.ParseError = "SKILL.md must be a regular file, not a symlink"
			skills = append(skills, skill)
			return nil
		}
		parsed, parseErr := parseSkillFile(s.root, skillFile, skill)
		if parseErr != nil {
			skill.ParseError = parseErr.Error()
			skills = append(skills, skill)
			return nil
		}
		skill = parsed
		references, referenceErr := readReferences(s.root, folder)
		if referenceErr != nil {
			skill.ParseError = referenceErr.Error()
		} else {
			skill.References = references
		}
		skills = append(skills, skill)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan skills root: %w", err)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].ID < skills[j].ID })
	return skills, nil
}

func (s *Store) skillID(folder string) (string, error) {
	relative, err := filepath.Rel(s.root, folder)
	if err != nil {
		return "", fmt.Errorf("resolve skill folder: %w", err)
	}
	id := filepath.ToSlash(relative)
	if id == "." || strings.HasPrefix(id, "../") || id == ".." {
		return id, errors.New("SKILL.md must be inside a category and skill folder")
	}
	if strings.Count(id, "/") != 1 {
		return id, fmt.Errorf("skill %q must have exactly one category and skill folder", id)
	}
	return id, nil
}

type frontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Version     string   `yaml:"version"`
	Author      string   `yaml:"author"`
	License     string   `yaml:"license"`
	Requires    []string `yaml:"requires"`
}

func parseSkillFile(root, filename string, base Skill) (Skill, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return Skill{}, fmt.Errorf("read SKILL.md: %w", err)
	}
	data, err := readBoundedRegularFileWithinRootAfterInspect(root, filename, maxSkillFileBytes, info)
	if err != nil {
		return Skill{}, fmt.Errorf("read SKILL.md: %w", err)
	}
	if !utf8.Valid(data) {
		return Skill{}, errors.New("SKILL.md must be UTF-8 text")
	}
	yamlText, body, err := splitFrontmatter(string(data))
	if err != nil {
		return Skill{}, err
	}
	decoder := yaml.NewDecoder(strings.NewReader(yamlText))
	decoder.KnownFields(true)
	var metadata frontmatter
	if err := decoder.Decode(&metadata); err != nil {
		return Skill{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Skill{}, errors.New("frontmatter must contain one YAML document")
		}
		return Skill{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	metadata.Name = strings.TrimSpace(metadata.Name)
	metadata.Description = strings.TrimSpace(metadata.Description)
	switch {
	case metadata.Name == "":
		return Skill{}, errors.New("frontmatter name is required")
	case metadata.Description == "":
		return Skill{}, errors.New("frontmatter description is required")
	case len([]rune(metadata.Name)) > maxNameRunes:
		return Skill{}, fmt.Errorf("frontmatter name exceeds %d characters", maxNameRunes)
	case len([]rune(metadata.Description)) > maxDescriptionRunes:
		return Skill{}, fmt.Errorf("frontmatter description exceeds %d characters", maxDescriptionRunes)
	}
	requires, err := normalizeCapabilities(metadata.Requires)
	if err != nil {
		return Skill{}, err
	}
	base.Name = metadata.Name
	base.Description = metadata.Description
	base.Version = strings.TrimSpace(metadata.Version)
	base.Author = strings.TrimSpace(metadata.Author)
	base.License = strings.TrimSpace(metadata.License)
	base.Requires = requires
	base.Body = strings.TrimSpace(body)
	contentHash := sha256.Sum256(data)
	revisionHash := sha256.Sum256([]byte(fmt.Sprintf(
		"%d:%d:%s:%x", info.ModTime().UnixNano(), info.Size(), statChangeToken(info), contentHash,
	)))
	base.Revision = fmt.Sprintf("%x", revisionHash)
	return base, nil
}

func splitFrontmatter(content string) (string, string, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return "", "", errors.New("SKILL.md must begin with YAML frontmatter")
	}
	rest := strings.TrimPrefix(content, "---\n")
	closing := strings.Index(rest, "\n---\n")
	if closing < 0 {
		if strings.HasSuffix(rest, "\n---") {
			return strings.TrimSuffix(rest, "\n---"), "", nil
		}
		return "", "", errors.New("SKILL.md frontmatter is not closed")
	}
	return rest[:closing], rest[closing+5:], nil
}

func normalizeCapabilities(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	capabilities := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len([]rune(value)) > maxCapabilityRunes || !capabilityPattern.MatchString(value) {
			return nil, fmt.Errorf("requires capability %q is invalid", value)
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		capabilities = append(capabilities, value)
	}
	sort.Strings(capabilities)
	return capabilities, nil
}

func readReferences(skillsRoot, skillFolder string) (map[string]string, error) {
	references := make(map[string]string)
	referencesRoot := filepath.Join(skillFolder, "references")
	rootInfo, err := os.Lstat(referencesRoot)
	if errors.Is(err, os.ErrNotExist) {
		return references, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect references folder: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, errors.New("references must be a real directory, not a symlink")
	}
	totalBytes := int64(0)
	err = filepath.WalkDir(referencesRoot, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == referencesRoot || entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("reference %q must not be a symlink", filepath.Base(filename))
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("reference %q must be a regular file", filepath.Base(filename))
		}
		if len(references) >= maxReferenceFiles {
			return fmt.Errorf("skill has more than %d reference files", maxReferenceFiles)
		}
		data, err := readBoundedRegularFileWithinRoot(skillsRoot, filename, maxReferenceFileBytes)
		if err != nil {
			return fmt.Errorf("read reference %q: %w", filepath.Base(filename), err)
		}
		if !utf8.Valid(data) {
			return fmt.Errorf("reference %q must be UTF-8 text", filepath.Base(filename))
		}
		totalBytes += int64(len(data))
		if totalBytes > maxReferenceTotalBytes {
			return fmt.Errorf("skill references exceed %d bytes", maxReferenceTotalBytes)
		}
		relative, err := filepath.Rel(skillFolder, filename)
		if err != nil {
			return err
		}
		references[filepath.ToSlash(relative)] = string(data)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return references, nil
}

func readBoundedRegularFileWithinRoot(root, filename string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	return readBoundedRegularFileWithinRootAfterInspect(root, filename, maximum, info)
}

func readBoundedRegularFileAfterInspect(filename string, maximum int64, info os.FileInfo) ([]byte, error) {
	return readBoundedRegularFileWithinRootAfterInspect(filepath.Dir(filename), filename, maximum, info)
}

func readBoundedRegularFileWithinRootAfterInspect(root, filename string, maximum int64, info os.FileInfo) ([]byte, error) {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("file must be regular and not a symlink")
	}
	if info.Size() > maximum {
		return nil, fmt.Errorf("file exceeds %d bytes", maximum)
	}
	fd, err := openFileWithinRoot(root, filename)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), filename)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open file descriptor")
	}
	defer func() { _ = file.Close() }()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, errors.New("file changed while it was being opened")
	}
	var buffer bytes.Buffer
	if _, err := io.Copy(&buffer, io.LimitReader(file, maximum+1)); err != nil {
		return nil, err
	}
	if int64(buffer.Len()) > maximum {
		return nil, fmt.Errorf("file exceeds %d bytes", maximum)
	}
	return buffer.Bytes(), nil
}

func openFileWithinRoot(root, filename string) (int, error) {
	relative, err := filepath.Rel(root, filename)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return -1, errors.New("file must remain inside the skills root")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return -1, err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return -1, errors.New("skills root must be a real directory")
	}
	currentFD, err := unix.Open(root, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	currentFile := os.NewFile(uintptr(currentFD), root)
	if currentFile == nil {
		_ = unix.Close(currentFD)
		return -1, errors.New("open skills root descriptor")
	}
	openedRootInfo, err := currentFile.Stat()
	if err != nil || !os.SameFile(rootInfo, openedRootInfo) {
		_ = currentFile.Close()
		if err != nil {
			return -1, err
		}
		return -1, errors.New("skills root changed while it was being opened")
	}

	components := strings.Split(relative, string(filepath.Separator))
	for _, component := range components[:len(components)-1] {
		nextFD, openErr := unix.Openat(currentFD, component, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			_ = currentFile.Close()
			return -1, openErr
		}
		nextFile := os.NewFile(uintptr(nextFD), component)
		if nextFile == nil {
			_ = unix.Close(nextFD)
			_ = currentFile.Close()
			return -1, errors.New("open directory descriptor")
		}
		_ = currentFile.Close()
		currentFD, currentFile = nextFD, nextFile
	}
	leafFD, err := unix.Openat(currentFD, components[len(components)-1], unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	_ = currentFile.Close()
	if err != nil {
		return -1, err
	}
	return leafFD, nil
}

func statChangeToken(info os.FileInfo) string {
	value := reflect.ValueOf(info.Sys())
	if !value.IsValid() {
		return ""
	}
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return ""
	}
	var token strings.Builder
	for _, name := range []string{"Dev", "Ino", "Ctim", "Ctimespec", "Ctime", "Ctimensec"} {
		field := value.FieldByName(name)
		if field.IsValid() && field.CanInterface() {
			fmt.Fprintf(&token, "%s=%v;", name, field.Interface())
		}
	}
	return token.String()
}
