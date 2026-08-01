Unicode true

!define INFO_PROJECTNAME "vibe-proxy-desktop"
!define INFO_COMPANYNAME "vibe-proxy"
!define INFO_PRODUCTNAME "Vibe Proxy"
!ifndef INFO_PRODUCTVERSION
  !define INFO_PRODUCTVERSION "0.1.0"
!endif
!ifndef INFO_PRODUCTVERSION_NUMERIC
  !define INFO_PRODUCTVERSION_NUMERIC "0.1.0"
!endif
!define INFO_COPYRIGHT "Copyright 2026 vibe-proxy contributors"
!define PRODUCT_EXECUTABLE "vibe-proxy-desktop.exe"
!define UNINST_KEY_NAME "vibe-proxy-vibe-proxy-desktop"
!define REQUEST_EXECUTION_LEVEL "user"
!define WAILS_INSTALL_SCOPE "user"

!include "wails_tools.nsh"
!include "MUI.nsh"

VIProductVersion "${INFO_PRODUCTVERSION_NUMERIC}.0"
VIFileVersion "${INFO_PRODUCTVERSION_NUMERIC}.0"
VIAddVersionKey "CompanyName" "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion" "${INFO_PRODUCTVERSION}"
VIAddVersionKey "ProductName" "${INFO_PRODUCTNAME}"
ManifestDPIAware true

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_ABORTWARNING
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\bin\vibe-proxy-desktop-${ARCH}-setup.exe"
InstallDir "$LOCALAPPDATA\Programs\Vibe Proxy"
ShowInstDetails show

Function .onInit
  !insertmacro wails.checkArchitecture
FunctionEnd

Section
  !insertmacro wails.setShellContext
  !insertmacro wails.webview2runtime
  SetOutPath $INSTDIR
  !insertmacro wails.files
  CreateShortcut "$SMPROGRAMS\Vibe Proxy.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
  !insertmacro wails.writeUninstaller
SectionEnd

Section "uninstall"
  !insertmacro wails.setShellContext
  ; Only remove installed program files. User data in
  ; %LOCALAPPDATA%\vibe-proxy is intentionally preserved.
  RMDir /r "$LOCALAPPDATA\Programs\Vibe Proxy"
  Delete "$SMPROGRAMS\Vibe Proxy.lnk"
  !insertmacro wails.deleteUninstaller
SectionEnd
