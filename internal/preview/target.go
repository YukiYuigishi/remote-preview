package preview

import (
	"context"
	"fmt"
	"html/template"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

type remoteTarget struct {
	Host    string
	Root    string
	Home    bool
	Local   bool
	Windows bool
}

func parseTarget(s string) (remoteTarget, error) {
	if isLocalTarget(s) {
		return remoteTarget{Root: s, Local: true}, nil
	}

	if !strings.Contains(s, ":") {
		if s == "" {
			return remoteTarget{}, fmt.Errorf("target must be a local path or host[:path]")
		}
		return remoteTarget{Host: s, Home: true}, nil
	}

	i := strings.IndexByte(s, ':')
	if i <= 0 || i == len(s)-1 {
		return remoteTarget{}, fmt.Errorf("target must be host[:path]")
	}

	host := s[:i]
	root := s[i+1:]
	if strings.HasPrefix(root, "//") {
		return remoteTarget{Host: host, Root: root}, nil
	}
	if strings.HasPrefix(root, "/") {
		return remoteTarget{Host: host, Root: path.Clean(root)}, nil
	}

	return remoteTarget{Host: host, Root: root, Home: true}, nil
}

func resolveLocalTarget(target remoteTarget) (remoteTarget, error) {
	if !target.Local {
		return target, nil
	}

	root := target.Root
	if root == "~" || strings.HasPrefix(root, "~/") || (runtime.GOOS == "windows" && strings.HasPrefix(root, `~\`)) {
		home, err := os.UserHomeDir()
		if err != nil {
			return remoteTarget{}, fmt.Errorf("resolve local home: %w", err)
		}
		if root == "~" {
			root = home
		} else {
			root = filepath.Join(home, root[2:])
		}
	}

	absolute, err := filepath.Abs(root)
	if err != nil {
		return remoteTarget{}, fmt.Errorf("resolve local target %q: %w", target.Root, err)
	}
	target.Host = "local"
	target.Root = filepath.Clean(absolute)
	return target, nil
}

func resolvePreviewTarget(ctx context.Context, target remoteTarget, remote RemoteFS) (remoteTarget, string, error) {
	kind, err := remote.Kind(ctx, target.Root)
	if err != nil {
		return remoteTarget{}, "", fmt.Errorf("resolve target %s:%s: %w", target.Host, target.Root, err)
	}
	switch kind {
	case "dir":
		return target, "/", nil
	case "file":
		var parent, name string
		if target.Local {
			parent = filepath.Dir(target.Root)
			name = filepath.Base(target.Root)
		} else {
			parent = dirRemotePath(target.Root)
			name = path.Base(target.Root)
		}
		target.Root = parent
		return target, "/" + url.PathEscape(name), nil
	case "missing":
		return remoteTarget{}, "", fmt.Errorf("target %s:%s does not exist", target.Host, target.Root)
	default:
		return remoteTarget{}, "", fmt.Errorf("target %s:%s is not a file or directory (kind %q)", target.Host, target.Root, kind)
	}
}

func (t *remoteTarget) setResolvedHome(home string) {
	if t.Root == "" || t.Root == "~" {
		t.Root = cleanRemotePath(home)
	} else {
		relative := strings.TrimPrefix(t.Root, "~/")
		t.Root = joinRemotePath(home, relative)
	}
	t.Home = false
}

func isWindowsDriveAbsolute(value string) bool {
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && value[2] == '/'
}

func isWindowsUNC(value string) bool {
	if !strings.HasPrefix(value, "//") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(value, "//"), "/")
	return len(parts) >= 2 && parts[0] != "" && parts[1] != ""
}

func cleanRemotePath(value string) string {
	if isWindowsUNC(value) {
		return "//" + path.Clean(strings.TrimPrefix(value, "//"))
	}
	clean := path.Clean(value)
	if isWindowsDriveAbsolute(value) && len(clean) == 2 && clean[1] == ':' {
		return clean + "/"
	}
	return clean
}

func joinRemotePath(base, relative string) string {
	if relative == "" {
		return cleanRemotePath(base)
	}
	return cleanRemotePath(base + "/" + relative)
}

func dirRemotePath(value string) string {
	if isWindowsUNC(value) {
		parts := strings.Split(strings.TrimPrefix(value, "//"), "/")
		if len(parts) <= 2 {
			return cleanRemotePath(value)
		}
		return "//" + path.Dir(strings.TrimPrefix(value, "//"))
	}
	dir := path.Dir(value)
	if isWindowsDriveAbsolute(value) && len(dir) == 2 && dir[1] == ':' {
		return dir + "/"
	}
	return dir
}

func (t *remoteTarget) prepareWindows() error {
	t.Windows = true
	t.Root = strings.ReplaceAll(t.Root, `\`, "/")
	if isWindowsDriveAbsolute(t.Root) || isWindowsUNC(t.Root) {
		wasUNC := isWindowsUNC(t.Root)
		t.Home = false
		t.Root = cleanRemotePath(t.Root)
		if wasUNC && !isWindowsUNC(t.Root) {
			return fmt.Errorf("windows UNC target must include a server and share")
		}
		return nil
	}
	if strings.HasPrefix(t.Root, "/") {
		return fmt.Errorf("windows server targets require a drive path such as host:C:/path")
	}
	if strings.Contains(t.Root, ":") {
		return fmt.Errorf("windows server targets require an absolute drive path such as host:C:/path")
	}
	return nil
}

func isWindowsSafeRelativePath(value string) bool {
	if strings.ContainsAny(value, "\\:\x00") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
			return false
		}
		stem, _, _ := strings.Cut(strings.ToUpper(segment), ".")
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9' {
			return false
		}
	}
	return true
}

func isLocalTarget(s string) bool {
	if s == "~" || strings.HasPrefix(s, "~/") {
		return true
	}
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(s, `~\`) || strings.HasPrefix(s, `.\`) || strings.HasPrefix(s, `..\`) || strings.HasPrefix(s, `\`) || filepath.VolumeName(s) != "" {
			return true
		}
	}
	if filepath.IsAbs(s) {
		return true
	}
	return s == "." || s == ".." || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") || strings.HasPrefix(s, "/")
}

func cleanRelativeURLPath(p string) string {
	clean := path.Clean("/" + p)
	return strings.TrimPrefix(clean, "/")
}

func pathURLJoin(base, name string) string {
	if base == "" {
		base = "/"
	}
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base + url.PathEscape(name)
}

func parentURL(current string) string {
	if current == "" || current == "/" {
		return ""
	}
	trimmed := strings.TrimSuffix(current, "/")
	lastSlash := strings.LastIndexByte(trimmed, '/')
	if lastSlash <= 0 {
		return "/"
	}
	return trimmed[:lastSlash+1]
}

func directoryTitle(rel string) string {
	if rel == "" {
		return "/"
	}
	return "/" + rel + "/"
}

func breadcrumbHTML(escapedPath string, directory bool) template.HTML {
	var b strings.Builder
	b.WriteString(`<a href="/">root</a>`)

	escapedPath = strings.Trim(escapedPath, "/")
	if escapedPath == "" {
		return template.HTML(b.String())
	}

	parts := strings.Split(escapedPath, "/")
	for i, escapedPart := range parts {
		if escapedPart == "" {
			continue
		}
		part, err := url.PathUnescape(escapedPart)
		if err != nil {
			part = escapedPart
		}

		b.WriteString(" <span>/</span> ")
		href := "/" + strings.Join(parts[:i+1], "/")
		isLast := i == len(parts)-1
		if !isLast || directory {
			href += "/"
		}
		if isLast {
			b.WriteString(template.HTMLEscapeString(part))
		} else {
			b.WriteString(`<a href="` + template.HTMLEscapeString(href) + `">` + template.HTMLEscapeString(part) + `</a>`)
		}
	}
	return template.HTML(b.String())
}

func withRawQuery(u *url.URL) string {
	q := u.Query()
	q.Set("raw", "1")
	copy := *u
	copy.RawQuery = q.Encode()
	return copy.RequestURI()
}
