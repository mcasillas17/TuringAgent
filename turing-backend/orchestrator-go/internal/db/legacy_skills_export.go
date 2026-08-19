package db

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

const importedSkillDescription = "Imported from the previous TuringAgent skill library."

var safeLegacySkillID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// legacyExportBeforeInstallHook is set only by the parent-swap regression.
var legacyExportBeforeInstallHook func()

type legacySkill struct {
	ID           string
	Name         string
	Instructions string
}

func exportLegacySkills(ctx context.Context, database *DB, skillsRoot string) error {
	return exportSkillRows(ctx, database, skillsRoot, `SELECT id, name, instructions FROM skills ORDER BY id`)
}

func exportRecoverySkills(ctx context.Context, database *DB, skillsRoot string) error {
	return exportSkillRows(ctx, database, skillsRoot, `SELECT id, name, instructions FROM legacy_skill_export_recovery ORDER BY id`)
}

func exportSkillRows(ctx context.Context, database *DB, skillsRoot, query string) error {
	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("read legacy skills: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var skills []legacySkill
	for rows.Next() {
		var skill legacySkill
		if err := rows.Scan(&skill.ID, &skill.Name, &skill.Instructions); err != nil {
			return fmt.Errorf("read legacy skill: %w", err)
		}
		skills = append(skills, skill)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read legacy skills: %w", err)
	}
	if len(skills) == 0 {
		return nil
	}
	if strings.TrimSpace(skillsRoot) == "" {
		return errors.New("skills root is required to export existing skills")
	}
	rootDirectory, err := openVerifiedExportRoot(skillsRoot)
	if err != nil {
		return err
	}
	defer func() { _ = rootDirectory.Close() }()
	importedDirectory, err := ensureExportDirectoryAt(rootDirectory, "imported")
	if err != nil {
		return err
	}
	defer func() { _ = importedDirectory.Close() }()
	for _, skill := range skills {
		content, err := marshalLegacySkill(skill)
		if err != nil {
			return err
		}
		folderName := legacySkillFolder(skill.ID)
		targetFolder, err := ensureExportDirectoryAt(importedDirectory, folderName)
		if err != nil {
			return err
		}
		if legacyExportBeforeInstallHook != nil {
			legacyExportBeforeInstallHook()
		}
		target := filepath.Join(skillsRoot, "imported", folderName, "SKILL.md")
		if err := writeExportOnceAt(targetFolder, "SKILL.md", target, content); err != nil {
			_ = targetFolder.Close()
			return err
		}
		if err := verifyExportDirectoryAt(rootDirectory, "imported", importedDirectory); err != nil {
			_ = targetFolder.Close()
			return err
		}
		if err := verifyExportDirectoryAt(importedDirectory, folderName, targetFolder); err != nil {
			_ = targetFolder.Close()
			return err
		}
		if err := targetFolder.Close(); err != nil {
			return err
		}
	}
	if err := verifyExportRootPath(skillsRoot, rootDirectory); err != nil {
		return err
	}
	return nil
}

func marshalLegacySkill(skill legacySkill) ([]byte, error) {
	metadata, err := yaml.Marshal(struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}{Name: skill.Name, Description: importedSkillDescription})
	if err != nil {
		return nil, fmt.Errorf("encode legacy skill metadata: %w", err)
	}
	var output bytes.Buffer
	output.WriteString("---\n")
	output.Write(metadata)
	output.WriteString("---\n")
	output.WriteString(strings.TrimSpace(skill.Instructions))
	output.WriteByte('\n')
	return output.Bytes(), nil
}

func legacySkillFolder(id string) string {
	if safeLegacySkillID.MatchString(id) && id != "." && id != ".." {
		return id
	}
	digest := sha256.Sum256([]byte(id))
	return fmt.Sprintf("skill-%x", digest[:8])
}

func writeExportOnceAt(directory *os.File, name, target string, content []byte) error {
	existing, exists, err := readExistingExportAt(directory, name, int64(len(content)+1))
	if err != nil {
		return fmt.Errorf("inspect existing skill export: %w", err)
	}
	if exists {
		if bytes.Equal(existing, content) {
			return nil
		}
		return fmt.Errorf("refusing to overwrite existing skill file: %s", target)
	}
	temporary, temporaryName, err := createTempExportAt(directory)
	if err != nil {
		return fmt.Errorf("create temporary skill export: %w", err)
	}
	defer func() { _ = unix.Unlinkat(int(directory.Fd()), temporaryName, 0) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure temporary skill export: %w", err)
	}
	if _, err := io.Copy(temporary, bytes.NewReader(content)); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary skill export: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary skill export: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary skill export: %w", err)
	}
	if err := unix.Linkat(int(directory.Fd()), temporaryName, int(directory.Fd()), name, 0); err != nil {
		if errors.Is(err, os.ErrExist) {
			existing, exists, readErr := readExistingExportAt(directory, name, int64(len(content)+1))
			if readErr != nil {
				return fmt.Errorf("inspect raced skill export: %w", readErr)
			}
			if exists && bytes.Equal(existing, content) {
				return nil
			}
			return fmt.Errorf("refusing to overwrite existing skill file: %s", target)
		}
		return fmt.Errorf("install skill export: %w", err)
	}
	return nil
}

func readExistingExportAt(directory *os.File, name string, maximum int64) ([]byte, bool, error) {
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, errors.New("existing skill export must be a regular file, not a symlink")
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, true, errors.New("open existing skill export")
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, true, err
	}
	if !info.Mode().IsRegular() {
		return nil, true, errors.New("existing skill export must be a regular file, not a symlink")
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum))
	if err != nil {
		return nil, true, err
	}
	return data, true, nil
}

func openVerifiedExportRoot(root string) (*os.File, error) {
	before, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect skills export root: %w", err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		return nil, fmt.Errorf("skills export root must be a real directory: %s", root)
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open skills export root: %w", err)
	}
	directory := os.NewFile(uintptr(fd), root)
	if directory == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open skills export root descriptor")
	}
	after, err := directory.Stat()
	if err != nil || !os.SameFile(before, after) {
		_ = directory.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("skills export root changed while it was opened")
	}
	return directory, nil
}

func ensureExportDirectoryAt(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		if err := unix.Mkdirat(int(parent.Fd()), name, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
			return nil, fmt.Errorf("create skills export directory %q: %w", name, err)
		}
		fd, err = unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	}
	if err != nil {
		return nil, fmt.Errorf("skills export path must be a real directory %q: %w", name, err)
	}
	directory := os.NewFile(uintptr(fd), name)
	if directory == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open skills export directory descriptor")
	}
	return directory, nil
}

func verifyExportDirectoryAt(parent *os.File, name string, expected *os.File) error {
	reopened, err := ensureExportDirectoryAt(parent, name)
	if err != nil {
		return fmt.Errorf("verify skills export directory %q: %w", name, err)
	}
	defer func() { _ = reopened.Close() }()
	want, err := expected.Stat()
	if err != nil {
		return err
	}
	got, err := reopened.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(want, got) {
		return fmt.Errorf("skills export directory %q changed during export", name)
	}
	return nil
}

func verifyExportRootPath(root string, expected *os.File) error {
	pathInfo, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("verify skills export root: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.IsDir() {
		return errors.New("skills export root changed during export")
	}
	openedInfo, err := expected.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(pathInfo, openedInfo) {
		return errors.New("skills export root changed during export")
	}
	return nil
}

func createTempExportAt(directory *os.File) (*os.File, string, error) {
	for attempt := 0; attempt < 100; attempt++ {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, "", err
		}
		name := ".skill-export-" + hex.EncodeToString(random[:])
		fd, err := unix.Openat(int(directory.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		file := os.NewFile(uintptr(fd), name)
		if file == nil {
			_ = unix.Close(fd)
			return nil, "", errors.New("open temporary export descriptor")
		}
		return file, name, nil
	}
	return nil, "", errors.New("could not allocate temporary export name")
}
