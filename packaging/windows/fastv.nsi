; FastV Windows installer (NSIS 3).
; Built by GitHub Actions with:
;   makensis /DAPP_VERSION=1.2.3 /DSOURCE_DIR=<dist> /DOUT_FILE=<out.exe> /DASSETS_DIR=<assets> packaging/windows/fastv.nsi
; The portable zip remains available for users who cannot run an installer.

Unicode true
!include "MUI2.nsh"
!include "FileFunc.nsh"

!ifndef APP_VERSION
  !define APP_VERSION "0.0.0-dev"
!endif
!ifndef APP_FILE_VERSION
  !define APP_FILE_VERSION "0.0.0.0"
!endif
!ifndef SOURCE_DIR
  !define SOURCE_DIR "dist"
!endif
!ifndef ASSETS_DIR
  !define ASSETS_DIR "assets"
!endif
!ifndef OUT_FILE
  !define OUT_FILE "dist/FastV-setup.exe"
!endif

!define PRODUCT_NAME "FastV"
!define PRODUCT_PUBLISHER "FastV"
!define PRODUCT_WEB_SITE "https://github.com/MardukPrime/Fast-V"
!define PRODUCT_UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\FastV"
!ifdef TEST_USER_INSTALL
  !define PRODUCT_UNINST_ROOT_KEY "HKCU"
  !define PRODUCT_APP_KEY_ROOT "HKCU"
!else
  !define PRODUCT_UNINST_ROOT_KEY "HKLM"
  !define PRODUCT_APP_KEY_ROOT "HKLM"
!endif

Name "${PRODUCT_NAME} ${APP_VERSION}"
OutFile "${OUT_FILE}"
InstallDir "$PROGRAMFILES64\${PRODUCT_NAME}"
!ifdef TEST_INSTALL_DIR
  InstallDir "${TEST_INSTALL_DIR}"
!endif
InstallDirRegKey HKLM "Software\FastV" "InstallDir"
!ifdef TEST_USER_INSTALL
  RequestExecutionLevel user
!else
  RequestExecutionLevel admin
!endif
SetCompressor /SOLID lzma
BrandingText "${PRODUCT_NAME} ${APP_VERSION}"
Icon "${ASSETS_DIR}\appicon.ico"
UninstallIcon "${ASSETS_DIR}\appicon.ico"
VIProductVersion "${APP_FILE_VERSION}"
VIAddVersionKey "ProductName" "${PRODUCT_NAME}"
VIAddVersionKey "FileDescription" "${PRODUCT_NAME} installer"
VIAddVersionKey "FileVersion" "${APP_VERSION}"
VIAddVersionKey "ProductVersion" "${APP_VERSION}"
VIAddVersionKey "LegalCopyright" "FastV contributors"

!define MUI_ABORTWARNING
!define MUI_ICON "${ASSETS_DIR}\appicon.ico"
!define MUI_UNICON "${ASSETS_DIR}\appicon.ico"
!define MUI_WELCOMEPAGE_TITLE "Welcome to the ${PRODUCT_NAME} ${APP_VERSION} installer"
!define MUI_WELCOMEPAGE_TEXT "This wizard installs ${PRODUCT_NAME}, a desktop Xray proxy client.$\r$\n$\r$\nThe installer only copies program files. Your subscriptions, nodes and settings are stored in your user profile and are never deleted by the uninstaller."
!define MUI_FINISHPAGE_RUN "$INSTDIR\Fast-V.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Start ${PRODUCT_NAME}"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "${SOURCE_DIR}\LICENSE"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"

Section "Install"
!ifdef TEST_USER_INSTALL
  SetShellVarContext current
!else
  SetShellVarContext all
!endif
  SetOutPath "$INSTDIR"
  File "${SOURCE_DIR}\Fast-V.exe"
  File "${SOURCE_DIR}\LICENSE"
  File "${SOURCE_DIR}\README.md"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateDirectory "$SMPROGRAMS\${PRODUCT_NAME}"
  CreateShortcut "$SMPROGRAMS\${PRODUCT_NAME}\${PRODUCT_NAME}.lnk" "$INSTDIR\Fast-V.exe"
  CreateShortcut "$SMPROGRAMS\${PRODUCT_NAME}\Uninstall ${PRODUCT_NAME}.lnk" "$INSTDIR\uninstall.exe"
  CreateShortcut "$DESKTOP\${PRODUCT_NAME}.lnk" "$INSTDIR\Fast-V.exe"

  WriteRegStr ${PRODUCT_APP_KEY_ROOT} "Software\FastV" "InstallDir" "$INSTDIR"
  WriteUninstaller "$INSTDIR\uninstall.exe"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "DisplayName" "${PRODUCT_NAME}"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "DisplayVersion" "${APP_VERSION}"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "Publisher" "${PRODUCT_PUBLISHER}"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "URLInfoAbout" "${PRODUCT_WEB_SITE}"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "DisplayIcon" "$INSTDIR\Fast-V.exe"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "UninstallString" "$INSTDIR\uninstall.exe"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegDWORD ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "NoModify" 1
  WriteRegDWORD ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "EstimatedSize" "$0"
SectionEnd

Section "Uninstall"
!ifdef TEST_USER_INSTALL
  SetShellVarContext current
!else
  SetShellVarContext all
!endif
  Delete "$DESKTOP\${PRODUCT_NAME}.lnk"
  Delete "$SMPROGRAMS\${PRODUCT_NAME}\${PRODUCT_NAME}.lnk"
  Delete "$SMPROGRAMS\${PRODUCT_NAME}\Uninstall ${PRODUCT_NAME}.lnk"
  RMDir "$SMPROGRAMS\${PRODUCT_NAME}"

  Delete "$INSTDIR\Fast-V.exe"
  Delete "$INSTDIR\LICENSE"
  Delete "$INSTDIR\README.md"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  DeleteRegKey ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}"
  DeleteRegKey ${PRODUCT_APP_KEY_ROOT} "Software\FastV"

  ; User data under %APPDATA%\FastV is intentionally preserved. The app also
  ; offers Settings -> System integration -> Environment cleanup for the
  ; protocol/autostart/PAC residues, and the data directory can be removed
  ; manually for a completely clean uninstall.
SectionEnd
