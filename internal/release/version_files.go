package release

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var (
	rootVersionLiteral = regexp.MustCompile(`(?m)(\bVersion:\s*)"([^"\r\n]*)"`)
	batsVersionLiteral = regexp.MustCompile(`(?m)(assert_output\s+"emberfall version )([^"\r\n]+)(")`)
)

type versionFileOperations struct {
	rename func(string, string) error
	remove func(string) error
	sync   func(*os.File) error
	write  func(*os.File, []byte) (int, error)
}

var operatingSystemVersionFiles = versionFileOperations{
	rename: os.Rename,
	remove: os.Remove,
	sync:   (*os.File).Sync,
	write:  (*os.File).Write,
}

type versionFileUpdate struct {
	path     string
	contents []byte
	original []byte
	mode     os.FileMode
	literal  string
	temp     string
}

// VersionFiles is a validated, not-yet-applied update of Emberfall's synchronized version literals.
type VersionFiles struct {
	updates []versionFileUpdate
	ops     versionFileOperations
}

// PrepareVersionFiles validates and prepares synchronized version-literal updates without writing either file.
func PrepareVersionFiles(root string, version Version) (*VersionFiles, error) {
	if err := validateVersion(version); err != nil {
		return nil, err
	}
	targets := []struct {
		path    string
		matcher *regexp.Regexp
		replace func([]byte) []byte
	}{
		{
			path:    filepath.Join(root, "cmd", "root.go"),
			matcher: rootVersionLiteral,
			replace: func(contents []byte) []byte {
				return rootVersionLiteral.ReplaceAll(contents, []byte(`${1}"`+version.String()+`"`))
			},
		},
		{
			path:    filepath.Join(root, "tests", "cli.bats"),
			matcher: batsVersionLiteral,
			replace: func(contents []byte) []byte {
				return batsVersionLiteral.ReplaceAll(contents, []byte(`${1}`+version.String()+`${3}`))
			},
		},
	}

	type validatedSource struct {
		path     string
		contents []byte
		mode     os.FileMode
		version  Version
		literal  string
		replace  func([]byte) []byte
	}
	sources := make([]validatedSource, 0, len(targets))
	for _, target := range targets {
		info, err := os.Lstat(target.path)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", target.path, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("version target %s must be a regular file", target.path)
		}
		contents, err := os.ReadFile(target.path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", target.path, err)
		}
		matches := target.matcher.FindAllSubmatchIndex(contents, -1)
		if len(matches) != 1 {
			return nil, fmt.Errorf("expected exactly one version literal in %s, found %d", target.path, len(matches))
		}
		literal := string(contents[matches[0][4]:matches[0][5]])
		current, err := ParseVersion(literal)
		if err != nil {
			return nil, fmt.Errorf("invalid current version in %s: %w", target.path, err)
		}
		sources = append(sources, validatedSource{
			path:     target.path,
			contents: contents,
			mode:     info.Mode(),
			version:  current,
			literal:  literal,
			replace:  target.replace,
		})
	}
	if sources[0].version != sources[1].version {
		return nil, fmt.Errorf("managed version literals disagree: %s has %s, %s has %s", sources[0].path, sources[0].version, sources[1].path, sources[1].version)
	}

	updates := make([]versionFileUpdate, 0, len(sources))
	for _, source := range sources {
		updates = append(updates, versionFileUpdate{
			path:     source.path,
			contents: source.replace(source.contents),
			original: append([]byte(nil), source.contents...),
			mode:     source.mode,
			literal:  source.literal,
		})
	}
	return &VersionFiles{updates: updates, ops: operatingSystemVersionFiles}, nil
}

// ValidateBaseline confirms that every managed source literal is exactly the
// version from which the release plan was derived.
func (files *VersionFiles) ValidateBaseline(previousVersion string) error {
	if files == nil || len(files.updates) == 0 {
		return fmt.Errorf("no prepared version files")
	}
	for _, update := range files.updates {
		if update.literal != previousVersion {
			return fmt.Errorf("managed version literal in %s is %s, expected previous version %s", update.path, update.literal, previousVersion)
		}
	}
	return nil
}

// Restore replaces every managed source with its exact prepared bytes and
// permissions, using atomic temporary-file replacements.
func (files *VersionFiles) Restore() error {
	if files == nil || len(files.updates) == 0 {
		return fmt.Errorf("no prepared version files")
	}
	return files.restoreThrough(len(files.updates))
}

// Apply writes every prepared version file or restores already-replaced files after an error.
func (files *VersionFiles) Apply() error {
	if files == nil || len(files.updates) == 0 {
		return fmt.Errorf("no prepared version files")
	}
	for index := range files.updates {
		temp, err := writeVersionTemp(files.updates[index].path, files.updates[index].contents, files.updates[index].mode, files.ops)
		if err != nil {
			return errors.Join(err, files.removeTemps())
		}
		files.updates[index].temp = temp
	}

	for index := range files.updates {
		if err := files.ops.rename(files.updates[index].temp, files.updates[index].path); err != nil {
			replaceErr := fmt.Errorf("replace %s: %w", files.updates[index].path, err)
			rollbackErr := files.restoreThrough(index)
			cleanupErr := files.removeTemps()
			if rollbackErr != nil {
				return errors.Join(replaceErr, fmt.Errorf("partial version-file state after rollback: %w", rollbackErr), cleanupErr)
			}
			return errors.Join(replaceErr, cleanupErr)
		}
		files.updates[index].temp = ""
	}
	return nil
}

// UpdateVersionFiles prepares then applies synchronized version-literal updates.
func UpdateVersionFiles(root string, version Version) error {
	files, err := PrepareVersionFiles(root, version)
	if err != nil {
		return err
	}
	return files.Apply()
}

func writeVersionTemp(path string, contents []byte, mode os.FileMode, ops versionFileOperations) (string, error) {
	temp, err := os.CreateTemp(filepath.Dir(path), ".emberfall-version-*")
	if err != nil {
		return "", fmt.Errorf("create replacement for %s: %w", path, err)
	}
	name := temp.Name()
	operationErr := temp.Chmod(mode.Perm())
	if operationErr == nil {
		written, writeErr := ops.write(temp, contents)
		if writeErr == nil && written != len(contents) {
			writeErr = io.ErrShortWrite
		}
		operationErr = writeErr
		if operationErr == nil {
			operationErr = ops.sync(temp)
		}
	}
	operationErr = errors.Join(operationErr, temp.Close())
	if operationErr != nil {
		removeErr := ops.remove(name)
		return "", errors.Join(
			fmt.Errorf("write replacement for %s: %w", path, operationErr),
			cleanupError(name, removeErr),
		)
	}
	return name, nil
}

func (files *VersionFiles) restoreThrough(last int) error {
	var restoreErrors []error
	for index := last - 1; index >= 0; index-- {
		update := files.updates[index]
		temp, err := writeVersionTemp(update.path, update.original, update.mode, files.ops)
		if err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("restore %s: %w", update.path, err))
			continue
		}
		if err := files.ops.rename(temp, update.path); err != nil {
			removeErr := files.ops.remove(temp)
			restoreErrors = append(restoreErrors, errors.Join(fmt.Errorf("restore %s: %w", update.path, err), cleanupError(temp, removeErr)))
		}
	}
	return errors.Join(restoreErrors...)
}

func (files *VersionFiles) removeTemps() error {
	var removeErrors []error
	for index := range files.updates {
		if files.updates[index].temp != "" {
			if err := files.ops.remove(files.updates[index].temp); err != nil && !errors.Is(err, os.ErrNotExist) {
				removeErrors = append(removeErrors, fmt.Errorf("remove temporary file %s: %w", files.updates[index].temp, err))
			}
			files.updates[index].temp = ""
		}
	}
	return errors.Join(removeErrors...)
}

func cleanupError(path string, err error) error {
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("remove failed temporary file %s: %w", path, err)
}
