package preview

import "html/template"

var directoryTemplate = template.Must(template.New("directory").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}} · {{.Host}}</title>
  <style>
    :root { color-scheme: light dark; font: 16px/1.5 system-ui, sans-serif; }
    body { max-width: 1100px; margin: 0 auto; padding: 1rem; }
    nav { margin-bottom: 1rem; overflow-wrap: anywhere; }
    nav a { text-decoration: none; }
    table { width: 100%; border-collapse: collapse; }
    th, td { border-bottom: 1px solid #8885; padding: .55rem .4rem; text-align: left; }
    th:last-child, td:last-child { text-align: right; color: #888; }
    .name { overflow-wrap: anywhere; }
    .actions { display: flex; gap: .8rem; margin-bottom: 1rem; }
  </style>
</head>
<body>
  <nav aria-label="Breadcrumb">{{.Breadcrumb}}</nav>
  <div class="actions">{{if .Parent}}<a href="{{.Parent}}">↑ Parent</a>{{end}}</div>
  <h1>{{.Title}}</h1>
  <p><code>{{.RemotePath}}</code></p>
  <table>
    <thead><tr><th>Name</th><th>Kind</th></tr></thead>
    <tbody>
    {{range .Entries}}
      <tr><td class="name">{{.Icon}} <a href="{{.Href}}">{{.Name}}</a></td><td>{{.Kind}}</td></tr>
    {{else}}
      <tr><td colspan="2">Empty directory</td></tr>
    {{end}}
    </tbody>
  </table>
</body>
</html>`))

var markdownTemplate = template.Must(template.New("markdown").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Name}} · {{.Host}}</title>
  <style>
    :root { color-scheme: light dark; font: 16px/1.5 system-ui, sans-serif; }
    body { max-width: 1000px; margin: 0 auto; padding: 1rem; }
    nav { margin-bottom: 1rem; overflow-wrap: anywhere; }
    nav a { text-decoration: none; }
    .toolbar { display: flex; gap: .8rem; margin-bottom: 1rem; }
    #rendered, #source { overflow-x: auto; }
    #source { white-space: pre-wrap; font: 13px/1.45 ui-monospace, monospace; }
    .notice { padding: .8rem; border: 1px solid #8888; border-radius: .4rem; }
  </style>
</head>
<body>
  <nav aria-label="Breadcrumb">{{.Breadcrumb}}</nav>
  <h1>{{.Name}}</h1>
  <p><code>{{.RemotePath}}</code></p>
  <div class="toolbar"><a href="{{.RawURL}}">Raw</a><button id="source-toggle" type="button">Source</button></div>
  <div id="notice" class="notice" hidden></div>
  <article id="rendered"></article>
  <pre id="source" hidden></pre>
  <script src="/_remote-preview/assets/v1/marked.js"></script>
  <script src="/_remote-preview/assets/v1/mermaid.js"></script>
  <script>
    const source = {{.SourceJSON}};
    const rendered = document.getElementById('rendered');
    const sourceView = document.getElementById('source');
    const notice = document.getElementById('notice');
    const showSource = (message) => {
      rendered.hidden = true;
      sourceView.hidden = false;
      sourceView.textContent = source;
      if (message) { notice.hidden = false; notice.textContent = message; }
    };
    document.getElementById('source-toggle').addEventListener('click', () => {
      const isHidden = sourceView.hidden;
      sourceView.hidden = !isHidden;
      rendered.hidden = isHidden;
    });
    try {
      if (!window.marked || !window.mermaid) {
        throw new Error('preview assets unavailable');
      }
      rendered.innerHTML = window.marked.parse(source, { gfm: true, breaks: false });
      window.mermaid.initialize({ startOnLoad: false });
      Promise.resolve(window.mermaid.run({ querySelector: 'pre code.language-mermaid' }))
        .catch(() => showSource('Rich preview could not be loaded. Showing Markdown source.'));
    } catch (_) {
      showSource('Rich preview could not be loaded. Showing Markdown source.');
    }
  </script>
</body>
</html>`))

var textTemplate = template.Must(template.New("text").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Name}} · {{.Host}}</title>
  <style>
    :root { color-scheme: light dark; font: 16px/1.5 system-ui, sans-serif; }
    body { max-width: 1200px; margin: 0 auto; padding: 1rem; }
    nav { margin-bottom: 1rem; overflow-wrap: anywhere; }
    nav a { text-decoration: none; }
    .toolbar { display: flex; gap: .8rem; margin-bottom: 1rem; }
    pre { overflow: auto; padding: 1rem; border-radius: .4rem; background: #8882; }
  </style>
  <link rel="stylesheet" href="/_remote-preview/assets/v1/highlight.css">
</head>
<body>
  <nav aria-label="Breadcrumb">{{.Breadcrumb}}</nav>
  <h1>{{.Name}}</h1>
  <p><code>{{.RemotePath}}</code></p>
  <div class="toolbar"><a href="{{.RawURL}}">Raw</a></div>
  <pre><code id="source-code" class="language-{{.Language}}">{{.Source}}</code></pre>
  <script src="/_remote-preview/assets/v1/highlight.js"></script>
  <script>
    const source = {{.SourceJSON}};
    const language = {{.LanguageJSON}};
    const sourceCode = document.getElementById('source-code');
    sourceCode.textContent = source;
    if (language && window.hljs && window.hljs.getLanguage(language)) {
      try {
        sourceCode.innerHTML = window.hljs.highlight(source, { language }).value;
      } catch (_) {
        sourceCode.textContent = source;
      }
    }
  </script>
</body>
</html>`))
