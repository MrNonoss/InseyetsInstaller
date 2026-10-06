# Inseyets Installer

A lightweight, portable Windows utility that silently installs forensic tools for instructors and lab administrators:

1. **DCode** (Digital Detective)
2. **Inseyets.PA** (Cellebrite)
3. **Inseyets.UFED** (Cellebrite)

**The tools installers are not provided here, and you must have them on your computer**

The installers are located automatically if available, you pick the options, and the tool runs them in order with a live log.

## Features

- **Automatic detection**: on launch, scans the tool folder and its parent (including sibling folders) for installers and pre-fills the paths. The highest version is selected when several are found.
- **Sequential silent installs** in a fixed order (DCode, then PA, then UFED), with a live console log, elapsed timer and progress indicator.
- **Per-tool options** for logging, validation, cloud/maps components and restart behavior (see [Command-line reference](#command-line-reference)).
- **Single portable executable**: no installation and no dependencies.
- **Runs as Administrator**: the app manifest requests elevation, so Windows shows a UAC prompt at launch.

## Requirements

- Windows 10 or 11 (64-bit)
- Administrator permissions
- The installer packages for the tools you want to install

## Usage

1. Place `InseyetsInstaller.exe` in (or next to) the folder containing the installers.
2. Run it and accept the UAC prompt.
3. Check the auto-detected paths, or use **Browse** / **Clear** to change them if needed.
4. Choose the options for each tool.
5. Click **Start Installation**.

### Detection rules

| Tool | Search depth | Match |
|------|--------------|-------|
| DCode | 7 | `DCode-*.exe` |
| Inseyets.PA | 4 | Installer with a `.7z` payload |
| Inseyets.UFED | 4 | Setup executable with a `.bin` payload |

## Command-line reference

These are the commands the tool runs for each installer.

### DCode

```
<installer.exe> /SP- /VERYSILENT /SUPPRESSMSGBOXES
```

### Inseyets.PA

```
<installer.exe> /install /silent /norestart [options]
```

| Option | Default | Effect |
|--------|---------|--------|
| Collect Logs | off | Adds `/log <toolDir>\PA_logs\PA_logs.txt` |
| Skip Validations | on | `SKIP_VALIDATIONS=true/false` |
| Install Cloud | on | `INSTALL_CLOUD=true/false` |
| Install Maps | on | `INSTALL_TILE_SERVER=true/false` |

### Inseyets.UFED

```
<installer.exe> /SP- /VERYSILENT /SUPPRESSMSGBOXES [options]
```

| Option | Default | Effect |
|--------|---------|--------|
| Collect Logs | off | Adds `/LOG=<toolDir>\UFED_logs\UFED_logs.txt` |
| Restart when done | off | When off, `/NORESTART` is added |

## Building from source

Requires [Go](https://go.dev/dl/) on Windows.

```cmd
build.bat
```

The script generates the Windows resources (icon, manifest, version info) with [go-winres](https://github.com/tc-hib/go-winres) and builds `InseyetsInstaller.exe`. Without the script, `go build -ldflags="-H windowsgui -s -w" -o InseyetsInstaller.exe .` works too, using the committed `rsrc_windows_*.syso` files.

To run the tests, use an elevated terminal, because the embedded manifest requires Administrator rights:

```cmd
go test ./...
```

## Antivirus notes

The executable is unsigned and launches third-party installers silently, so Microsoft Defender or other antivirus products may flag it, especially on machines that have never seen it. If that happens:

- Verify you downloaded the file from this repository's [Releases](../../releases) page.
- Add an exclusion for the tool's folder.
- Report false positives to [Microsoft](https://www.microsoft.com/wdsi/filesubmission).

## Project layout

```
main.go          Application source (UI, detection, installation)
detector_test.go Tests for installer detection and argument building
build.bat        Build script
winres/          Windows manifest, icon and version info
assets/          Logos and icons
```

## License

No license has been chosen yet. Add a `LICENSE` file to define how others may use this project.
