package usbgadget

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"

	"github.com/rs/zerolog"
)

type symlink struct {
	Path   string
	Target string
}

func compareSymlinks(expected []symlink, actual []symlink) bool {
	if len(expected) != len(actual) {
		return false
	}

	return reflect.DeepEqual(expected, actual)
}

func checkIfSymlinksInOrder(fc *FileChange, logger *zerolog.Logger) (FileState, error) {
	if logger == nil {
		logger = defaultLogger
	}
	l := logger.With().Str("path", fc.Path).Logger()

	if len(fc.ParamSymlinks) == 0 {
		return FileStateUnknown, fmt.Errorf("no symlinks to check")
	}

	fi, err := os.Lstat(fc.Path)

	if err != nil {
		if os.IsNotExist(err) {
			return FileStateAbsent, nil
		} else {
			l.Warn().Err(err).Msg("failed to stat file")
			return FileStateUnknown, fmt.Errorf("failed to stat file")
		}
	}

	if !fi.IsDir() {
		return FileStateUnknown, fmt.Errorf("file is not a directory")
	}

	files, err := os.ReadDir(fc.Path)
	symlinks := make([]symlink, 0)
	if err != nil {
		return FileStateUnknown, fmt.Errorf("failed to read directory")
	}

	for _, file := range files {
		if file.Type()&os.ModeSymlink != os.ModeSymlink {
			continue
		}

		path := filepath.Join(fc.Path, file.Name())
		target, err := os.Readlink(path)
		if err != nil {
			return FileStateUnknown, fmt.Errorf("failed to read symlink")
		}

		if !filepath.IsAbs(target) {
			target = filepath.Join(fc.Path, target)
			newTarget, err := filepath.Abs(target)
			if err != nil {
				return FileStateUnknown, fmt.Errorf("failed to get absolute path")
			}
			target = newTarget
		}

		symlinks = append(symlinks, symlink{
			Path:   path,
			Target: target,
		})
	}

	// compare the symlinks with the expected symlinks
	if compareSymlinks(fc.ParamSymlinks, symlinks) {
		return FileStateSymlinkInOrderConfigFS, nil
	}

	l.Trace().Interface("expected", fc.ParamSymlinks).Interface("actual", symlinks).Msg("symlinks are not in order")

	return FileStateSymlinkNotInOrderConfigFS, nil
}

func recreateSymlinks(fc *FileChange, logger *zerolog.Logger) error {
	if logger == nil {
		logger = defaultLogger
	}
	// remove all symlinks
	files, err := os.ReadDir(fc.Path)
	if err != nil {
		return fmt.Errorf("failed to read directory")
	}

	l := logger.With().Str("path", fc.Path).Logger()
	l.Info().Msg("recreate symlinks")

	for _, file := range files {
		if file.Type()&os.ModeSymlink != os.ModeSymlink {
			continue
		}
		l.Info().Str("name", file.Name()).Msg("remove symlink")
		err := os.Remove(path.Join(fc.Path, file.Name()))
		if err != nil {
			return fmt.Errorf("failed to remove symlink")
		}
	}

	l.Info().Interface("param-symlinks", fc.ParamSymlinks).Msg("create symlinks")

	// create the symlinks
	for _, symlink := range fc.ParamSymlinks {
		l.Info().Str("name", symlink.Path).Str("target", symlink.Target).Msg("create symlink")

		path := symlink.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(fc.Path, path)
		}

		err := os.Symlink(symlink.Target, path)
		if err != nil {
			l.Warn().Err(err).Msg("failed to create symlink")
			return fmt.Errorf("failed to create symlink")
		}
	}

	return nil
}
