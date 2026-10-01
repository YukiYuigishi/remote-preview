package preview

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"
)

const powerShellCommandPrefix = "powershell.exe -NoProfile -NonInteractive -EncodedCommand "

func encodedPowerShellCommand(script string) string {
	units := utf16.Encode([]rune(script))
	data := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(data[i*2:], unit)
	}
	return powerShellCommandPrefix + base64.StdEncoding.EncodeToString(data)
}

func psQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func windowsScript(body string) string {
	return "$ErrorActionPreference='Stop';try{" + body + "}catch{[Console]::Error.Write($_.Exception.Message);exit 1}"
}

func (s *sshRemoteFS) runWindows(ctx context.Context, operation, remotePath, body string) ([]byte, error) {
	return s.runNamed(ctx, operation, remotePath, "@powershell", windowsScript(body))
}

func (s *sshRemoteFS) probeWindows(ctx context.Context) bool {
	out, err := s.runWindows(ctx, "windows_probe", "", "if([Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT){[Console]::Out.Write('windows')}")
	return err == nil && string(out) == "windows"
}

func (s *sshRemoteFS) windowsHome(ctx context.Context) (string, error) {
	out, err := s.runWindows(ctx, "home", "", `$homePath=[Environment]::GetFolderPath('UserProfile').Replace('\','/');$data=[Text.Encoding]::UTF8.GetBytes($homePath);$out=[Console]::OpenStandardOutput();$out.Write($data,0,$data.Length)`)
	if err != nil {
		return "", err
	}
	home := string(out)
	if !isWindowsDriveAbsolute(home) && !isWindowsUNC(home) {
		return "", fmt.Errorf("remote Windows home is not an absolute path: %q", home)
	}
	return cleanRemotePath(home), nil
}

func (s *sshRemoteFS) windowsKind(ctx context.Context, remotePath string) (string, error) {
	body := "$p=" + psQuote(remotePath) + `;if([IO.Directory]::Exists($p)){$kind='dir'}elseif([IO.File]::Exists($p)){$kind='file'}else{$kind='missing'};[Console]::Out.Write($kind)`
	out, err := s.runWindows(ctx, "kind", remotePath, body)
	return string(out), err
}

func (s *sshRemoteFS) windowsSize(ctx context.Context, remotePath string) (int64, error) {
	body := "$p=" + psQuote(remotePath) + `;$info=[IO.FileInfo]::new($p);if(!$info.Exists){throw 'file does not exist'};[Console]::Out.Write($info.Length)`
	out, err := s.runWindows(ctx, "size", remotePath, body)
	if err != nil {
		return 0, err
	}
	size, err := strconv.ParseInt(string(out), 10, 64)
	if err != nil || size < 0 {
		return 0, fmt.Errorf("invalid remote file size %q", out)
	}
	return size, nil
}

func (s *sshRemoteFS) windowsRead(ctx context.Context, remotePath string) ([]byte, error) {
	body := "$p=" + psQuote(remotePath) + `;$file=[IO.File]::OpenRead($p);try{$out=[Console]::OpenStandardOutput();$file.CopyTo($out)}finally{$file.Dispose()}`
	return s.runWindows(ctx, "read", remotePath, body)
}

func (s *sshRemoteFS) windowsOpenRange(ctx context.Context, remotePath string, offset, length int64) (io.ReadCloser, error) {
	if offset < 0 || length < -1 {
		return nil, fmt.Errorf("invalid file range")
	}
	body := "$p=" + psQuote(remotePath) + ";$offset=" + strconv.FormatInt(offset, 10) + ";$length=" + strconv.FormatInt(length, 10) + `;
$file=[IO.File]::OpenRead($p)
try {
  if($offset -gt $file.Length){throw 'file range starts past end of file'}
  [void]$file.Seek($offset,[IO.SeekOrigin]::Begin)
  $out=[Console]::OpenStandardOutput()
  if($length -lt 0){$file.CopyTo($out)}else{
    $buffer=New-Object byte[] 32768
    $remaining=$length
    while($remaining -gt 0){
      $wanted=[int][Math]::Min($buffer.Length,$remaining)
      $read=$file.Read($buffer,0,$wanted)
      if($read -eq 0){break}
      $out.Write($buffer,0,$read)
      $remaining-=$read
    }
  }
} finally {$file.Dispose()}`
	return s.openNamed(ctx, "range_read", remotePath, "@powershell", windowsScript(body))
}

const windowsWritePathCheck = `
function Check-WritePath([string]$target) {
  if($target -notmatch '^(?:[A-Za-z]:[\/]|[\/]{2}[^\/]+[\/][^\/]+)') { throw 'upload path must be absolute' }
  foreach($part in $target.Split([char[]]@('\','/'),[StringSplitOptions]::RemoveEmptyEntries)) {
    if($part -eq '.' -or $part -eq '..') { throw 'upload path must be normalized' }
  }
  $full=[IO.Path]::GetFullPath($target)
  $root=[IO.Path]::GetPathRoot($full)
  if(!$root) { throw 'upload path must be absolute' }
  $current=$root
  $rest=$full.Substring($root.Length)
  foreach($part in $rest.Split([char[]]@('\','/'),[StringSplitOptions]::RemoveEmptyEntries)) {
    $current=[IO.Path]::Combine($current,$part)
    if([IO.File]::Exists($current) -or [IO.Directory]::Exists($current)) {
      $attributes=[IO.File]::GetAttributes($current)
      if(($attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'refusing to follow a reparse point in upload path' }
    }
  }
}
`

func (s *sshRemoteFS) windowsMkdirAll(ctx context.Context, remotePath string) error {
	body := windowsWritePathCheck + "$p=" + psQuote(remotePath) + `;Check-WritePath $p;[void][IO.Directory]::CreateDirectory($p)`
	_, err := s.runWindows(ctx, "mkdir", remotePath, body)
	return err
}

func (s *sshRemoteFS) windowsWriteFile(ctx context.Context, remotePath string, src io.Reader, nonce string) error {
	body := windowsWritePathCheck + "$p=" + psQuote(remotePath) + ";$nonce=" + psQuote(nonce) + `;
Check-WritePath $p
$parent=[IO.Path]::GetDirectoryName($p)
if(![IO.Directory]::Exists($parent)){throw 'upload parent does not exist'}
if([IO.Directory]::Exists($p)){throw 'upload destination is a directory'}
$temp=[IO.Path]::Combine($parent,'.ykview-upload-'+$nonce)
$backup=[IO.Path]::Combine($parent,'.ykview-backup-'+$nonce)
try {
  if([IO.File]::Exists($backup)){throw 'upload backup path already exists'}
  $file=[IO.File]::Open($temp,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None)
  try {[Console]::OpenStandardInput().CopyTo($file)} finally {$file.Dispose()}
  Check-WritePath $p
  if([IO.File]::Exists($p)){[IO.File]::Replace($temp,$p,$backup)}else{[IO.File]::Move($temp,$p)}
} finally {
  if([IO.File]::Exists($temp)){[IO.File]::Delete($temp)}
  if([IO.File]::Exists($backup)){[IO.File]::Delete($backup)}
}`
	_, err := s.runNamedReader(ctx, "upload", remotePath, src, false, "@powershell", windowsScript(body))
	return err
}

const windowsBatchListingScript = `
$utf8=[Text.UTF8Encoding]::new($false)
$out=[Console]::OpenStandardOutput()
function Record([string[]]$fields){
  foreach($field in $fields){$data=$utf8.GetBytes($field);$out.Write($data,0,$data.Length);$out.WriteByte(0)}
}
function Kind([string]$p){
  if([IO.Directory]::Exists($p)){return 'dir'}
  if([IO.File]::Exists($p)){return 'file'}
  return 'missing'
}
function Listing([string]$directory,[string]$relative){
  Record @('D',$relative)
  foreach($entry in [IO.Directory]::EnumerateFileSystemEntries($directory)){
    Record @('E',(Kind $entry),[IO.Path]::GetFileName($entry))
  }
  Record @('X')
}
$rootKind=Kind $root
Record @('K',$rootKind)
if($rootKind -eq 'dir'){
  Listing $root ''
  $count=0
  foreach($entry in [IO.Directory]::EnumerateFileSystemEntries($root)){
    if($count -ge $maxChildren){break}
    if((Kind $entry) -eq 'dir'){
      Listing $entry ([IO.Path]::GetFileName($entry))
      $count++
    }
  }
}
`

func (s *sshRemoteFS) windowsListBatch(ctx context.Context, remotePath string) (batchListingResult, error) {
	body := "$root=" + psQuote(remotePath) + ";$maxChildren=" + strconv.Itoa(batchMaxDirectories) + ";" + windowsBatchListingScript
	out, err := s.runWindows(ctx, "list_batch_windows", remotePath, body)
	if err != nil {
		return batchListingResult{}, err
	}
	return parseBatchListings(remotePath, out)
}

func (s *sshRemoteFS) windowsList(ctx context.Context, remotePath string) ([]remoteEntry, error) {
	result, err := s.windowsListBatch(ctx, remotePath)
	if err != nil {
		return nil, err
	}
	for _, listing := range result.Listings {
		if listing.Path == cleanRemotePath(remotePath) {
			return listing.Entries, nil
		}
	}
	return nil, fmt.Errorf("windows directory listing missing %q", remotePath)
}
