Unicode true

!define WAILS_WIN10_REQUIRED "$(WindowsVersionError)"
!define WAILS_ARCHITECTURE_NOT_SUPPORTED "$(ArchitectureError)"
!define WAILS_INSTALL_WEBVIEW_DETAILPRINT "$(InstallingWebView)"
!include "wails_tools.nsh"

!define PRODUCT_WEBSITE "https://github.com/AntNoHuabei/Miel"

VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion "${INFO_PRODUCTVERSION}.0"
VIAddVersionKey "CompanyName" "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion" "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion" "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright" "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName" "${INFO_PRODUCTNAME}"

ManifestDPIAware true
SetCompressor /SOLID lzma
SetCompressorDictSize 32
BrandingText "Miel"
XPStyle on
ShowInstDetails hide
ShowUninstDetails hide

!include "MUI2.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_ABORTWARNING
!define MUI_UNABORTWARNING
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "$(FinishRun)"
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchApplication
!define MUI_FINISHPAGE_LINK "$(ProjectWebsite)"
!define MUI_FINISHPAGE_LINK_LOCATION "${PRODUCT_WEBSITE}"

!if "${WAILS_INSTALL_SCOPE}" == "user"
    !define MUI_LANGDLL_REGISTRY_ROOT "HKCU"
!else
    !define MUI_LANGDLL_REGISTRY_ROOT "HKLM"
!endif
!define MUI_LANGDLL_REGISTRY_KEY "${UNINST_KEY}"
!define MUI_LANGDLL_REGISTRY_VALUENAME "InstallerLanguage"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_WELCOME
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_UNPAGE_FINISH

!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_RESERVEFILE_LANGDLL

LangString SectionCore ${LANG_ENGLISH} "Miel application files"
LangString SectionCore ${LANG_SIMPCHINESE} "Miel 应用程序文件"
LangString SectionDesktop ${LANG_ENGLISH} "Desktop shortcut"
LangString SectionDesktop ${LANG_SIMPCHINESE} "桌面快捷方式"
LangString SectionCoreDescription ${LANG_ENGLISH} "Install Miel and its required local runtimes."
LangString SectionCoreDescription ${LANG_SIMPCHINESE} "安装 Miel 及其所需的本地运行环境。"
LangString SectionDesktopDescription ${LANG_ENGLISH} "Create a shortcut to Miel on the desktop."
LangString SectionDesktopDescription ${LANG_SIMPCHINESE} "在桌面创建 Miel 快捷方式。"
LangString FinishRun ${LANG_ENGLISH} "Launch Miel"
LangString FinishRun ${LANG_SIMPCHINESE} "立即启动 Miel"
LangString ProjectWebsite ${LANG_ENGLISH} "Visit the Miel project page"
LangString ProjectWebsite ${LANG_SIMPCHINESE} "访问 Miel 项目主页"
LangString UninstallShortcut ${LANG_ENGLISH} "Uninstall Miel"
LangString UninstallShortcut ${LANG_SIMPCHINESE} "卸载 Miel"
LangString WindowsVersionError ${LANG_ENGLISH} "Miel requires Windows 10 or later."
LangString WindowsVersionError ${LANG_SIMPCHINESE} "Miel 需要 Windows 10 或更高版本。"
LangString ArchitectureError ${LANG_ENGLISH} "This installer does not support the current Windows architecture."
LangString ArchitectureError ${LANG_SIMPCHINESE} "此安装程序不支持当前的 Windows 系统架构。"
LangString InstallingWebView ${LANG_ENGLISH} "Installing Microsoft Edge WebView2 Runtime"
LangString InstallingWebView ${LANG_SIMPCHINESE} "正在安装 Microsoft Edge WebView2 运行时"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
!if "${WAILS_INSTALL_SCOPE}" == "user"
    InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
    InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation"
!else
    InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
    InstallDirRegKey HKLM "${UNINST_KEY}" "InstallLocation"
!endif

Function .onInit
    !insertmacro MUI_LANGDLL_DISPLAY
    !insertmacro wails.checkArchitecture
FunctionEnd

Function un.onInit
    !insertmacro MUI_UNGETLANGUAGE
FunctionEnd

Function LaunchApplication
!if "${REQUEST_EXECUTION_LEVEL}" == "admin"
    ExecShell "" "$WINDIR\explorer.exe" '"$INSTDIR\${PRODUCT_EXECUTABLE}"'
!else
    Exec '"$INSTDIR\${PRODUCT_EXECUTABLE}"'
!endif
FunctionEnd

Section "$(SectionCore)" SecCore
    SectionIn RO
    !insertmacro wails.setShellContext
    !insertmacro wails.webview2runtime

    SetOutPath "$INSTDIR"
    SetOverwrite on
    !insertmacro wails.files

    SetOutPath "$INSTDIR\runtime"
    File /r "..\runtime-stage\*.*"
    SetOutPath "$INSTDIR"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols
    !insertmacro wails.writeUninstaller

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    CreateDirectory "$SMPROGRAMS\${INFO_PRODUCTNAME}"
    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}\Uninstall ${INFO_PRODUCTNAME}.lnk"
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}\卸载 ${INFO_PRODUCTNAME}.lnk"
    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}\$(UninstallShortcut).lnk" "$INSTDIR\uninstall.exe"

!if "${WAILS_INSTALL_SCOPE}" == "user"
    WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKCU "${UNINST_KEY}" "URLInfoAbout" "${PRODUCT_WEBSITE}"
    WriteRegStr HKCU "${UNINST_KEY}" "HelpLink" "${PRODUCT_WEBSITE}/issues"
    WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
    WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1
!else
    WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKLM "${UNINST_KEY}" "URLInfoAbout" "${PRODUCT_WEBSITE}"
    WriteRegStr HKLM "${UNINST_KEY}" "HelpLink" "${PRODUCT_WEBSITE}/issues"
    WriteRegDWORD HKLM "${UNINST_KEY}" "NoModify" 1
    WriteRegDWORD HKLM "${UNINST_KEY}" "NoRepair" 1
!endif
SectionEnd

Section "$(SectionDesktop)" SecDesktop
    !insertmacro wails.setShellContext
    CreateShortcut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
SectionEnd

!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
    !insertmacro MUI_DESCRIPTION_TEXT ${SecCore} "$(SectionCoreDescription)"
    !insertmacro MUI_DESCRIPTION_TEXT ${SecDesktop} "$(SectionDesktopDescription)"
!insertmacro MUI_FUNCTION_DESCRIPTION_END

Section "Uninstall"
    !insertmacro wails.setShellContext

    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}\${INFO_PRODUCTNAME}.lnk"
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}\Uninstall ${INFO_PRODUCTNAME}.lnk"
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}\卸载 ${INFO_PRODUCTNAME}.lnk"
    RMDir "$SMPROGRAMS\${INFO_PRODUCTNAME}"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols
    !insertmacro wails.deleteUninstaller

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}"
    RMDir /r "$INSTDIR"
SectionEnd
