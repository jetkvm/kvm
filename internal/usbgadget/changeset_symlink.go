package usbgadget

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"

	"github.com/jetkvm/kvm/internal/logging"
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

func checkIfSymlinksInOrder(fc *FileChange, loggingContext *logging.Context) (FileState, error) {
	context := loggingContext.Str("path", fc.Path)

	if len(fc.ParamSymlinks) == 0 {
		return FileStateUnknown, fmt.Errorf("no symlinks to check")
	}

	fi, err := os.Lstat(fc.Path)

	if err != nil {
		if os.IsNotExist(err) {
			return FileStateAbsent, nil
		} else {
			context.Err(err).Warn().Msg("failed to stat file")
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

	context.Interface("expected", fc.ParamSymlinks).Interface("actual", symlinks).Trace().Msg("symlinks are not in order")

	return FileStateSymlinkNotInOrderConfigFS, nil
}

func recreateSymlinks(fc *FileChange, context *logging.Context) error {
	// remove all symlinks
	files, err := os.ReadDir(fc.Path)
	if err != nil {
		return fmt.Errorf("failed to read directory")
	}

	context = context.Str("path", fc.Path)
	context.Info().Msg("recreate symlinks")

	for _, file := range files {
		if file.Type()&os.ModeSymlink != os.ModeSymlink {
			continue
		}

		context.Str("name", file.Name()).Info().Msg("remove symlink")
		err := os.Remove(path.Join(fc.Path, file.Name()))
		if err != nil {
			return fmt.Errorf("failed to remove symlink")
		}
	}

	context = context.Interface("param-symlinks", fc.ParamSymlinks)
	context.Debug().Msg("create symlinks")

	// create the symlinks
	for _, symlink := range fc.ParamSymlinks {
		eachContext := context.Str("name", symlink.Path).Str("target", symlink.Target)
		eachContext.Debug().Msg("create symlink")

		path := symlink.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(fc.Path, path)
		}

		err := os.Symlink(symlink.Target, path)
		if err != nil {
			eachContext.Err(err).Warn().Msg("failed to create symlink")
			return fmt.Errorf("failed to create symlink")
		}
	}

	return nil
}
