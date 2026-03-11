package db

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

type migrationFile struct {
	name    string
	content []byte
}

func RunMigrations(db *sql.DB, migrationsDir string) error {
	files, err := readMigrations(migrationsDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if _, err := db.Exec(string(file.content)); err != nil {
			return fmt.Errorf("apply migration %s: %w", file.name, err)
		}
	}
	return nil
}

func readMigrations(migrationsDir string) ([]migrationFile, error) {
	migrationsDir = strings.TrimSpace(migrationsDir)
	if migrationsDir != "" {
		files, err := readMigrationsFromDir(migrationsDir)
		if err == nil {
			return files, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return readEmbeddedMigrations()
}

func readMigrationsFromDir(migrationsDir string) ([]migrationFile, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("read migrations directory: %w", err)
	}

	var files []migrationFile
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		fullPath := filepath.Join(migrationsDir, entry.Name())
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", fullPath, err)
		}
		files = append(files, migrationFile{name: entry.Name(), content: content})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}

func readEmbeddedMigrations() ([]migrationFile, error) {
	entries, err := fs.ReadDir(embeddedMigrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	var files []migrationFile
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		path := "migrations/" + entry.Name()
		content, err := fs.ReadFile(embeddedMigrations, path)
		if err != nil {
			return nil, fmt.Errorf("read embedded migration %s: %w", entry.Name(), err)
		}
		files = append(files, migrationFile{name: entry.Name(), content: content})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}
