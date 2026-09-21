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
    .actions, .upload-actions { display: flex; flex-wrap: wrap; gap: .8rem; margin-bottom: 1rem; align-items: center; }
    .transfer-panel { padding: .8rem; margin: 1rem 0; border: 1px solid #8888; border-radius: .4rem; }
    .drop-zone { padding: 1rem; border: 2px dashed #8888; border-radius: .4rem; text-align: center; }
    .drop-zone.dragover { border-color: #4488ff; background: #4488ff22; }
    progress { width: min(100%, 24rem); }
  </style>
</head>
<body>
  <nav aria-label="Breadcrumb">{{.Breadcrumb}}</nav>
  <div class="actions">
    {{if .Parent}}<a href="{{.Parent}}">↑ Parent</a>{{end}}
    <button id="download-selected" type="button">Download selected (.zip)</button>
  </div>
  <h1>{{.Title}}</h1>
  <p><code>{{.RemotePath}}</code></p>
  {{if .WriteEnabled}}
  <section class="transfer-panel" aria-label="Upload">
    <div class="upload-actions">
      <strong>Upload</strong>
      <label><input id="file-picker" type="file" multiple hidden>Choose files</label>
      <label><input id="directory-picker" type="file" webkitdirectory directory multiple hidden>Choose directory</label>
    </div>
    <div id="drop-zone" class="drop-zone">Drop files or a directory here</div>
    <p><progress id="upload-progress" max="100" value="0" hidden></progress> <span id="upload-status" role="status"></span></p>
  </section>
  {{end}}
  <table>
    <thead><tr><th><input id="select-all" type="checkbox" aria-label="Select all"></th><th>Name</th><th>Kind</th><th>Action</th></tr></thead>
    <tbody>
    {{range .Entries}}
      <tr>
        <td><input class="entry-select" type="checkbox" data-path="{{.SelectPath}}" aria-label="Select {{.Name}}"></td>
        <td class="name">{{.Icon}} <a href="{{.Href}}">{{.Name}}</a></td>
        <td>{{.Kind}}</td>
        <td><a href="{{.DownloadURL}}">Download</a></td>
      </tr>
    {{else}}
      <tr><td colspan="4">Empty directory</td></tr>
    {{end}}
    </tbody>
  </table>
  <div id="transfer-config" data-upload-url="{{.UploadURL}}"></div>
  <script>
    (() => {
      const selected = () => Array.from(document.querySelectorAll('.entry-select:checked'));
      const selectAll = document.getElementById('select-all');
      selectAll?.addEventListener('change', () => {
        selected();
        document.querySelectorAll('.entry-select').forEach((checkbox) => { checkbox.checked = selectAll.checked; });
      });
      document.getElementById('download-selected')?.addEventListener('click', () => {
        const paths = selected().map((checkbox) => checkbox.dataset.path).filter(Boolean);
        if (!paths.length) {
          window.alert('Select at least one file or directory.');
          return;
        }
        const query = new URLSearchParams();
        paths.forEach((entryPath) => query.append('path', entryPath));
        window.location.href = '/_ykview/transfer/download-zip?' + query.toString();
      });

      const config = document.getElementById('transfer-config');
      const filePicker = document.getElementById('file-picker');
      const directoryPicker = document.getElementById('directory-picker');
      const dropZone = document.getElementById('drop-zone');
      const progress = document.getElementById('upload-progress');
      const status = document.getElementById('upload-status');
      if (!config || !filePicker || !directoryPicker || !dropZone) return;

      const pickerRecords = (input) => Array.from(input.files || []).map((file) => ({
        file,
        path: file.webkitRelativePath || file.name,
      }));
      const readEntries = (reader) => new Promise((resolve, reject) => {
        const all = [];
        const read = () => reader.readEntries((entries) => {
          if (!entries.length) { resolve(all); return; }
          all.push(...entries);
          read();
        }, reject);
        read();
      });
      const walkEntry = async (entry, prefix, output) => {
        if (entry.isFile) {
          await new Promise((resolve, reject) => entry.file((file) => {
            output.push({ file, path: prefix + file.name });
            resolve();
          }, reject));
          return;
        }
        if (entry.isDirectory) {
          const children = await readEntries(entry.createReader());
          for (const child of children) await walkEntry(child, prefix + entry.name + '/', output);
        }
      };
      const dropRecords = async (dataTransfer) => {
        const items = Array.from(dataTransfer.items || []);
        const entries = items.map((item) => item.webkitGetAsEntry && item.webkitGetAsEntry()).filter(Boolean);
        if (!entries.length) return Array.from(dataTransfer.files || []).map((file) => ({ file, path: file.name }));
        const output = [];
        for (const entry of entries) await walkEntry(entry, '', output);
        return output;
      };
      const upload = (records) => {
        if (!records.length) return;
        if (!window.confirm('Upload ' + records.length + ' file(s) to this directory? Existing regular files will be replaced.')) {
          status.textContent = 'Upload cancelled.';
          return;
        }
        const form = new FormData();
        records.forEach((record) => {
          form.append('files', record.file, record.file.name);
          form.append('paths', record.path);
        });
        const xhr = new XMLHttpRequest();
        const total = records.reduce((sum, record) => sum + record.file.size, 0);
        progress.hidden = false;
        progress.value = 0;
        status.textContent = 'Uploading…';
        xhr.upload.onprogress = (event) => {
          if (event.lengthComputable) progress.value = Math.round(event.loaded / event.total * 100);
          else if (total) progress.value = Math.min(99, Math.round(event.loaded / total * 100));
        };
        xhr.onload = () => {
          let response = {};
          try { response = JSON.parse(xhr.responseText); } catch (_) {}
          if (xhr.status >= 200 && xhr.status < 300) {
            progress.value = 100;
            status.textContent = 'Uploaded ' + (response.uploaded?.length || records.length) + ' file(s). Reloading…';
            window.setTimeout(() => window.location.reload(), 300);
          } else {
            status.textContent = response.error || ('Upload failed (' + xhr.status + ').');
          }
        };
        xhr.onerror = () => { status.textContent = 'Upload failed.'; };
        xhr.onabort = () => { status.textContent = 'Upload cancelled.'; };
        xhr.open('POST', config.dataset.uploadUrl);
        xhr.send(form);
      };
      filePicker.addEventListener('change', () => upload(pickerRecords(filePicker)));
      directoryPicker.addEventListener('change', () => upload(pickerRecords(directoryPicker)));
      dropZone.addEventListener('dragover', (event) => { event.preventDefault(); dropZone.classList.add('dragover'); });
      dropZone.addEventListener('dragleave', () => dropZone.classList.remove('dragover'));
      dropZone.addEventListener('drop', async (event) => {
        event.preventDefault();
        dropZone.classList.remove('dragover');
        try { upload(await dropRecords(event.dataTransfer)); }
        catch (_) { status.textContent = 'Could not read dropped files.'; }
      });
    })();
  </script>
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
  <script src="/_ykview/assets/v1/marked.js"></script>
  <script src="/_ykview/assets/v1/mermaid.js"></script>
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
  <link rel="stylesheet" href="/_ykview/assets/v1/highlight.css">
</head>
<body>
  <nav aria-label="Breadcrumb">{{.Breadcrumb}}</nav>
  <h1>{{.Name}}</h1>
  <p><code>{{.RemotePath}}</code></p>
  <div class="toolbar"><a href="{{.RawURL}}">Raw</a></div>
  <pre><code id="source-code" class="language-{{.Language}}">{{.Source}}</code></pre>
  <script src="/_ykview/assets/v1/highlight.js"></script>
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
