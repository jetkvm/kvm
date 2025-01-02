package plugin

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const pluginsExtractsFolder = pluginsFolder + "/extracts"

func init() {
	_ = os.MkdirAll(pluginsExtractsFolder, 0755)
}

func extractPlugin(filePath string) (*string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file for extraction: %v", err)
	}
	defer file.Close()

	var reader io.Reader = file
	// TODO: there's probably a better way of doing this without relying on the file extension
	if strings.HasSuffix(filePath, ".gz") {
		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %v", err)
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	destinationFolder := path.Join(pluginsExtractsFolder, uuid.New().String())
	if err := os.MkdirAll(destinationFolder, 0755); err != nil {
		return nil, fmt.Errorf("failed to create extracts folder: %v", err)
	}

	tarReader := tar.NewReader(reader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar header: %v", err)
		}

		// Prevent path traversal attacks
		targetPath := filepath.Join(destinationFolder, header.Name)
		if !strings.HasPrefix(targetPath, filepath.Clean(destinationFolder)+string(os.PathSeparator)) {
			return nil, fmt.Errorf("tar file contains illegal path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return nil, fmt.Errorf("failed to create directory: %v", err)
			}
		case tar.TypeReg:
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return nil, fmt.Errorf("failed to create file: %v", err)
			}
			defer file.Close()

			if _, err := io.Copy(file, tarReader); err != nil {
				return nil, fmt.Errorf("failed to extract file: %v", err)
			}
		default:
			return nil, fmt.Errorf("unsupported tar entry type: %v", header.Typeflag)
		}
	}

	return &destinationFolder, nil
}
