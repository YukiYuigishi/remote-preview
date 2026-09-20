// Package remotehelper implements the short-lived filesystem helper executed
// on the remote host by ykview.
package remotehelper

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

const ProtocolVersion = "1"

// Run executes the helper command and writes the NUL-delimited batch protocol
// to out. The helper intentionally only supports a bounded, non-recursive
// listing operation.
func Run(args []string, out io.Writer) error {
	if len(args) != 4 || args[0] != "list-batch" || args[1] != ProtocolVersion {
		return fmt.Errorf("usage: list-batch %s <path> <max-directories>", ProtocolVersion)
	}
	maxDirectories, err := strconv.Atoi(args[3])
	if err != nil || maxDirectories < 0 {
		return fmt.Errorf("invalid max-directories: %q", args[3])
	}

	writer := bufio.NewWriter(out)
	root := args[2]
	rootKind := rootKind(root)
	if err := writeRecord(writer, "K", rootKind); err != nil {
		return err
	}
	if rootKind != "dir" {
		return writer.Flush()
	}

	if err := writeListing(writer, root, ""); err != nil {
		return err
	}

	children, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read root directory: %w", err)
	}
	count := 0
	for _, child := range children {
		if count >= maxDirectories {
			break
		}
		childPath := filepath.Join(root, child.Name())
		if entryKind(childPath) != "dir" {
			continue
		}
		if err := writeListing(writer, childPath, child.Name()); err != nil {
			return err
		}
		count++
	}

	return writer.Flush()
}

func writeListing(writer *bufio.Writer, directory, relativePath string) error {
	if err := writeRecord(writer, "D", relativePath); err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read directory %q: %w", directory, err)
	}
	for _, entry := range entries {
		kind := entryKind(filepath.Join(directory, entry.Name()))
		if kind == "missing" {
			continue
		}
		if err := writeRecord(writer, "E", kind, entry.Name()); err != nil {
			return err
		}
	}
	return writeRecord(writer, "X")
}

func writeRecord(writer *bufio.Writer, fields ...string) error {
	for _, field := range fields {
		if _, err := writer.WriteString(field); err != nil {
			return err
		}
		if err := writer.WriteByte(0); err != nil {
			return err
		}
	}
	return nil
}

func rootKind(name string) string {
	info, err := os.Stat(name)
	if err == nil {
		switch {
		case info.IsDir():
			return "dir"
		case info.Mode().IsRegular():
			return "file"
		}
		return "missing"
	}
	if isSymlink(name) {
		return "link"
	}
	return "missing"
}

func entryKind(name string) string {
	info, err := os.Stat(name)
	if err == nil {
		switch {
		case info.IsDir():
			return "dir"
		case info.Mode().IsRegular():
			return "file"
		default:
			return "other"
		}
	}
	if isSymlink(name) {
		return "link"
	}
	return "missing"
}

func isSymlink(name string) bool {
	info, err := os.Lstat(name)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}
