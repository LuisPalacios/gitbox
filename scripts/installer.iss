; gitbox — Inno Setup installer script
; Produces: gitbox-win-amd64-setup.exe
; Version is injected via GITBOX_VERSION env var by CI.

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
ChangesEnvironment=yes
PrivilegesRequired=admin
WizardStyle=modern
SetupIconFile=..\assets\icon.ico
UninstallDisplayIcon={app}\GitboxApp.exe
ArchitecturesInstallIn64BitMode=x64compatible

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
Source: "..\tmp-win\gitbox.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\tmp-win\GitboxApp.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\gitbox"; Filename: "{app}\GitboxApp.exe"; Comment: "Manage Git multi-account environments"
Name: "{group}\Uninstall gitbox"; Filename: "{uninstallexe}"

[Registry]
; Add install dir to system PATH (only if not already present).
Root: HKLM; Subkey: "SYSTEM\CurrentControlSet\Control\Session Manager\Environment"; \
  ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; \
  Check: NeedsAddPath(ExpandConstant('{app}'))

[Code]
function NeedsAddPath(Param: string): boolean;
var
  OrigPath: string;
begin
  if not RegQueryStringValue(HKLM,
    'SYSTEM\CurrentControlSet\Control\Session Manager\Environment',
    'Path', OrigPath) then
  begin
    Result := True;
    exit;
  end;
  Result := Pos(';' + Param + ';', ';' + OrigPath + ';') = 0;
end;

[Run]
Filename: "{app}\GitboxApp.exe"; Description: "Launch gitbox"; \
  Flags: nowait postinstall skipifsilent
