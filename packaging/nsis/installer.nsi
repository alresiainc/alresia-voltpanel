!define APPNAME "VoltPanel"
OutFile "voltpanel-installer.exe"
InstallDir "$PROGRAMFILES\VoltPanel"
Section
  SetOutPath $INSTDIR
  File /oname=voltpanel.exe "dist\voltpanel.exe"
  CreateShortCut "$DESKTOP\VoltPanel.lnk" "$INSTDIR\voltpanel.exe"
SectionEnd
