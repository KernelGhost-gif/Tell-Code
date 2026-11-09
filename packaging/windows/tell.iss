#define AppName "Tell Programming Language"
#ifndef AppVersion
  #define AppVersion "0.4.0"
#endif
#ifndef SourceDir
  #define SourceDir "..\dist"
#endif

[Setup]
AppId={{A37A4E83-1F2D-4AC4-BB27-6A3B1F572C26}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher=Tell Language contributors
DefaultDirName={autopf}\Tell
DefaultGroupName=Tell
OutputDir=..\..\dist
OutputBaseFilename=Tell-Setup-{#AppVersion}-windows-x64
ArchitecturesAllowed=x64
ArchitecturesInstallIn64BitMode=x64
PrivilegesRequired=admin
UninstallDisplayIcon={app}\tell.exe
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ChangesEnvironment=yes

[Tasks]
Name: "modifypath"; Description: "Add Tell to the system PATH"; Flags: checkedonce

[Files]
Source: "{#SourceDir}\tell-windows-amd64.exe"; DestDir: "{app}"; DestName: "tell.exe"; Flags: ignoreversion

[Registry]
Root: HKLM; Subkey: "SYSTEM\CurrentControlSet\Control\Session Manager\Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Tasks: modifypath; Check: NeedsAddPath('{app}')

[Code]
function NeedsAddPath(Param: String): Boolean;
var
  ExistingPath: String;
begin
  if not RegQueryStringValue(HKLM, 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment', 'Path', ExistingPath) then
    ExistingPath := '';
  Result := Pos(';' + Uppercase(Param) + ';', ';' + Uppercase(ExistingPath) + ';') = 0;
end;
