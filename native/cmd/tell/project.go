package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var validProjectName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

func projectFiles(name string) map[string]string {
	return map[string]string{
		"tell.toml": fmt.Sprintf("name = %q\nversion = \"0.1.0\"\nentrypoint = \"main.tell\"\n", name),
		"main.tell": fmt.Sprintf("Say \"Hello from %s!\".\n", name),
		filepath.Join("tests", "smoke_test.tell"): "Check that 1 plus 1 is equal to 2.\n",
	}
}

func initializeProject(directory, name string) (err error) {
	if !validProjectName.MatchString(name) {
		return fmt.Errorf("project name must start with a letter and contain only letters, numbers, hyphens, and underscores")
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("cannot initialize project directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("project target is not a directory: %s", root)
	}
	files := projectFiles(name)
	for relative := range files {
		if _, err := os.Lstat(filepath.Join(root, relative)); err == nil {
			return fmt.Errorf("refusing to overwrite existing project file %s", filepath.Join(root, relative))
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	testsDirectory := filepath.Join(root, "tests")
	createdTestsDirectory := false
	if info, err := os.Stat(testsDirectory); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("project tests path is not a directory: %s", testsDirectory)
		}
	} else if os.IsNotExist(err) {
		if err := os.Mkdir(testsDirectory, 0o755); err != nil {
			return err
		}
		createdTestsDirectory = true
	} else {
		return err
	}

	created := make([]string, 0, len(files))
	defer func() {
		if err != nil {
			for _, path := range created {
				_ = os.Remove(path)
			}
			if createdTestsDirectory {
				_ = os.Remove(testsDirectory)
			}
		}
	}()
	for relative, contents := range files {
		path := filepath.Join(root, relative)
		file, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if createErr != nil {
			return createErr
		}
		created = append(created, path)
		if _, err = file.WriteString(contents); err != nil {
			_ = file.Close()
			return err
		}
		if err = file.Close(); err != nil {
			return err
		}
	}
	return nil
}

func createProject(parent, name string) (string, error) {
	if !validProjectName.MatchString(name) {
		return "", fmt.Errorf("project name must start with a letter and contain only letters, numbers, hyphens, and underscores")
	}
	root, err := filepath.Abs(parent)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, name)
	if filepath.Dir(target) != root {
		return "", fmt.Errorf("project name must be a single directory name")
	}
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("project directory already exists: %s", target)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	stage, err := os.MkdirTemp(root, ".tell-project-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	if err := os.Chmod(stage, 0o755); err != nil {
		return "", err
	}
	if err := initializeProject(stage, name); err != nil {
		return "", err
	}
	if err := os.Rename(stage, target); err != nil {
		return "", err
	}
	return target, nil
}

func projectNameFromDirectory(directory string) string {
	name := filepath.Base(filepath.Clean(directory))
	name = strings.TrimSuffix(name, filepath.Ext(name))
	return name
}
