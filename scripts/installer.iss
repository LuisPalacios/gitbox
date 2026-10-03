; gitbox — Inno Setup installer script
; Produces: gitbox-win-amd64-setup.exe
; Version is injected via GITBOX_VERSION env var by CI.
;
; v2 installs only the GitboxApp GUI. Upgrading over a v1 install (same
; AppId) removes the stale gitbox.exe CLI and the {app} entry v1 appended
; to the machine PATH.

#define MyAppName "gitbox"
#define MyAppVersion GetEnv('GITBOX_VERSION')
#define MyAppPublisher "Luis Palacios"
#define MyAppURL "https://github.com/LuisPalacios/gitbox"

[Setup]
AppId={{8B2F4E3A-1C5D-4F8E-9A7B-3D6E2F1A8C4B}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}/issues
DefaultDirName={autopf}\gitbox
DefaultGroupName=gitbox
OutputBaseFilename=gitbox-win-amd64-setup
OutputDir=..\release
Compression=lzma2
SolidCompression=yes
; Broadcasts the PATH change when an upgrade from v1 removes {app} from it.
ChangesEnvironment=yes
PrivilegesRequired=admin
WizardStyle=modern
SetupIconFile=..\assets\icon.ico
UninstallDisplayIcon={app}\GitboxApp.exe
ArchitecturesInstallIn64BitMode=x64compatible

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[InstallDelete]
; v1 shipped the gitbox CLI next to the GUI. v2 has no CLI.
Type: files; Name: "{app}\gitbox.exe"

[Files]
Source: "..\tmp-win\GitboxApp.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\gitbox"; Filename: "{app}\GitboxApp.exe"; Comment: "Manage Git multi-account environments"
Name: "{group}\Uninstall gitbox"; Filename: "{uninstallexe}"

[Code]
const
  EnvironmentKey = 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment';

// v1 appended ";<app dir>" to the machine PATH so the gitbox CLI resolved
// from any shell. v2 has no CLI, so drop that entry, matched exactly the way
// v1 wrote and checked it. Every other entry stays as it is, and nothing is
// written when the entry is absent, so a fresh install or a repeat upgrade
// is a no-op. The PATH is wrapped in ";" so the first and last entries
// match like any other, then unwrapped.
procedure RemoveAppFromPath();
var
  Path, Entry: string;
  Found: Boolean;
begin
  if not RegQueryStringValue(HKLM, EnvironmentKey, 'Path', Path) then
    exit;
  Entry := ';' + ExpandConstant('{app}') + ';';
  Path := ';' + Path + ';';
  Found := False;
  while StringChangeEx(Path, Entry, ';', True) > 0 do
    Found := True;
  if Found then
    RegWriteExpandStringValue(HKLM, EnvironmentKey, 'Path', Copy(Path, 2, Length(Path) - 2));
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
    RemoveAppFromPath();
end;

[Run]
Filename: "{app}\GitboxApp.exe"; Description: "Launch gitbox"; \
  Flags: nowait postinstall skipifsilent
