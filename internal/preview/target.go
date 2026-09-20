package preview

import (
	"context"
	"fmt"
	"html/template"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type remoteTarget struct {
	Host  string
	Root  string
	Home  bool
	Local bool
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
	if strings.HasPrefix(root, "/") {
		return remoteTarget{Host: host, Root: path.Clean(root)}, nil
	}

	return remoteTarget{Host: host, Root: root, Home: true}, nil
}

func resolveTarget(ctx context.Context, target remoteTarget, remote RemoteFS) (remoteTarget, error) {
	if !target.Home {
		return target, nil
	}

	home, err := remote.Home(ctx)
	if err != nil {
		return remoteTarget{}, err
	}
	target.setResolvedHome(home)
	return target, nil
}

func resolveLocalTarget(target remoteTarget) (remoteTarget, error) {
	if !target.Local {
		return target, nil
	}

	root := target.Root
	if root == "~" || strings.HasPrefix(root, "~/") {
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

func (t *remoteTarget) setResolvedHome(home string) {
	if t.Root == "" || t.Root == "~" {
		t.Root = path.Clean(home)
	} else {
		relative := strings.TrimPrefix(t.Root, "~/")
		t.Root = path.Clean(path.Join(home, relative))
	}
	t.Home = false
}

func isLocalTarget(s string) bool {
	if s == "~" || strings.HasPrefix(s, "~/") {
		return true
	}
	if filepath.IsAbs(s) {
		return true
	}
	return s == "." || s == ".." || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../")
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
