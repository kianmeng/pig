package repo

import (
	"archive/tar"
	"compress/gzip"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"pig/internal/config"
	"strings"

	"github.com/sirupsen/logrus"
)

// Cache creates a compressed tarball of specified subdirectories under dirPath.
// 1) Closes files immediately after processing (no deferred file close).
// 2) Preserves symbolic links and empty directories in the tarball.
// 3) Ensures no upper-level references (e.g. "../../") are included.
// 4) Prints a warning if a specified subdirectory or file does not exist, then skips it.
// 5) Shows progress based on the total number of regular files and prints the file being processed.
// 6) Prints total file size in GiB before starting the compression process.
func Cache(dirPath, pkgPath string, repos []string) error {
	logrus.Infof("caching repo %s %s to %s", dirPath, strings.Join(repos, ","), pkgPath)

	// Use defaults if not provided
	if dirPath == "" {
		dirPath = "/www"
	}
	if pkgPath == "" {
		pkgPath = "/tmp/pkg.tgz"
	}
	if len(repos) == 0 {
		repos = []string{"pigsty"}
	}

	// Get absolute base directory (used to prevent upper-level references)
	absBase, err := filepath.Abs(dirPath)
	if err != nil {
		return fmt.Errorf("failed to determine abs path of dirPath %s: %v", dirPath, err)
	}

	// Check if base dir exists
	baseInfo, err := os.Stat(absBase)
	if os.IsNotExist(err) || !baseInfo.IsDir() {
		logrus.Warnf("base directory does not exist or is not a directory: %s", absBase)
		return fmt.Errorf("invalid base directory: %s", absBase)
	}

	// Count the total number of regular files and total size for progress & estimate
	totalFiles, totalSize, err := countFiles(absBase, repos)
	if err != nil {
		return fmt.Errorf("failed to count files: %v", err)
	}

	// Print estimated total file size in GiB (1 GiB = 1024^3 bytes)
	logrus.Infof("Estimated total file size: %.2f GiB", bytesToGiB(totalSize))

	// Remove existing tarball if any
	if err := os.RemoveAll(pkgPath); err != nil {
		return fmt.Errorf("failed to remove existing package %s: %v", pkgPath, err)
	}

	// Create the target tarball file
	outFile, err := os.Create(pkgPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %v", pkgPath, err)
	}
	defer outFile.Close()

	// Create a gzip writer with best compression
	gzipWriter, err := gzip.NewWriterLevel(outFile, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("failed to create gzip writer: %v", err)
	}
	defer gzipWriter.Close()

	// Create a tar writer
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	processed := 0 // number of files processed

	// Traverse each specified repository subdirectory
	for _, subDirName := range repos {
		subDirPath := filepath.Join(absBase, subDirName)
		if _, statErr := os.Stat(subDirPath); os.IsNotExist(statErr) {
			logrus.Warnf("subdirectory does not exist, skipping: %s", subDirPath)
			continue
		}

		err = filepath.Walk(subDirPath, func(filePath string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			// Ensure file path is within absBase (no "../" references)
			absFilePath, err := filepath.Abs(filePath)
			if err != nil {
				return fmt.Errorf("failed to get abs path for %s: %v", filePath, err)
			}
			if !strings.HasPrefix(absFilePath, absBase) {
				logrus.Warnf("file path references an upper-level directory, skipping: %s", absFilePath)
				return nil
			}

			// Compute tar header name (relative to base dir)
			relPath, err := filepath.Rel(absBase, absFilePath)
			if err != nil {
				return fmt.Errorf("failed to get relative path for %s: %v", filePath, err)
			}

			// Create tar header
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return fmt.Errorf("failed to create file info header for %s: %v", filePath, err)
			}
			header.Name = relPath

			// If it's a symbolic link, store its target
			if info.Mode()&os.ModeSymlink != 0 {
				linkTarget, readErr := os.Readlink(filePath)
				if readErr != nil {
					return fmt.Errorf("failed to read symlink %s: %v", filePath, readErr)
				}
				header.Linkname = linkTarget
				header.Typeflag = tar.TypeSymlink
			}

			// Write header
			if err := tarWriter.WriteHeader(header); err != nil {
				return fmt.Errorf("failed to write tar header for %s: %v", filePath, err)
			}

			// If it's a regular file, write its content
			if info.Mode().IsRegular() {
				f, openErr := os.Open(filePath)
				if openErr != nil {
					logrus.Warnf("failed to open file, skipping: %s, error: %v", filePath, openErr)
					return nil
				}

				_, copyErr := io.Copy(tarWriter, f)
				f.Close() // Close immediately after use
				if copyErr != nil {
					logrus.Warnf("failed to copy file content, skipping: %s, error: %v", filePath, copyErr)
					return nil
				}

				processed++
				printProgressAndFile(processed, totalFiles, relPath)
			}
			return nil
		})
		if err != nil {
			logrus.Warnf("failed to walk subdirectory %s: %v", subDirPath, err)
			continue
		}
	}

	fmt.Println()

	logrus.Infof("offline package created")
	_ = printFinalPackageInfo(pkgPath)
	return nil
}

// printFinalPackageInfo prints the final package path, size in GiB, and its MD5 checksum.
func printFinalPackageInfo(pkgPath string) error {
	fileInfo, err := os.Stat(pkgPath)
	if err != nil {
		return err
	}

	// use absolute path
	pkgPath, err = filepath.Abs(pkgPath)
	if err != nil {
		return err
	}

	logrus.Infof("offline package path : %s", pkgPath)
	logrus.Infof("offline package size : %.2f GiB", bytesToGiB(fileInfo.Size()))

	// Compute MD5 checksum
	f, err := os.Open(pkgPath)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := md5.New()
	if _, err := io.Copy(hash, f); err != nil {
		return err
	}
	logrus.Infof("offline package md5  : %x", hash.Sum(nil))

	osarch := config.OSArch
	switch config.OSArch {
	case "amd64":
		osarch = "x86_64"
	case "arm64":
		osarch = "aarch64"
	}
	inferName := fmt.Sprintf("pigsty-pkg-v%s.%s.%s.tgz", config.PigstyVersion, config.OSCode, osarch)
	logrus.Infof("offline package arch : %s", osarch)
	logrus.Infof("recommended pkg name : %s", inferName)
	return nil
}

// countFiles returns the total number of regular files and the total size (bytes) in the specified subdirectories.
func countFiles(absBase string, repos []string) (int, int64, error) {
	var totalFiles int
	var totalSize int64

	for _, subDirName := range repos {
		subDirPath := filepath.Join(absBase, subDirName)
		info, err := os.Stat(subDirPath)
		if os.IsNotExist(err) {
			logrus.Warnf("subdirectory does not exist for counting, skipping: %s", subDirPath)
			continue
		}
		if err != nil {
			return 0, 0, err
		}

		// If the path is actually a file, count it as well
		if !info.IsDir() {
			if info.Mode().IsRegular() {
				totalFiles++
				totalSize += info.Size()
			}
			continue
		}

		// Otherwise, walk the directory
		err = filepath.Walk(subDirPath, func(filePath string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.Mode().IsRegular() {
				totalFiles++
				totalSize += info.Size()
			}
			return nil
		})
		if err != nil {
			return 0, 0, err
		}
	}
	return totalFiles, totalSize, nil
}

// printProgressAndFile prints a file-based progress indicator with the current file name.
// The terminal line is cleared on each update to avoid leftover characters.
func printProgressAndFile(processed, total int, currentFile string) {
	if total == 0 {
		return
	}
	percent := float64(processed) / float64(total) * 100
	// \r    : Move cursor to the beginning of the line
	// \033[K: Clear from cursor to the end of the line (ANSI escape sequence)
	fmt.Printf("\r\033[KProgress: %d/%d (%.1f%%)  Adding: %s", processed, total, percent, currentFile)
	_ = os.Stdout.Sync() // Force flush
}

// bytesToGiB converts bytes to GiB (1 GiB = 1024^3 bytes).
func bytesToGiB(b int64) float64 {
	return float64(b) / (1024.0 * 1024.0 * 1024.0)
}
