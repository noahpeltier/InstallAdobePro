# InstallAdobePro

Written in golang

Downloads the latest version of the Adobe Acrobat Pro DC msi installer
Installer has a -readonlymode switch wich will disable pro features and suppresses the signin window.

If you want to build this:
```cmd
go build -o install_acrobat_universal.exe
```

## Example
install_acrobat_universal.exe -readonlymode
