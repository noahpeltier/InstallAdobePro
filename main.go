package main

import (
	"archive/zip"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
)

// Function to set Acrobat in read-only mode
func setAcrobatReadOnlyMode() error {
	baseKeyPath := `SOFTWARE\Policies\Adobe\Adobe Acrobat\DC\FeatureLockDown`
	cIPMKeyPath := baseKeyPath + `\cIPM`

	baseKey, _, err := registry.CreateKey(registry.LOCAL_MACHINE, baseKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open or create base key: %v", err)
	}
	defer baseKey.Close()

	err = baseKey.SetDWordValue("bIsSCReducedModeEnforcedEx", 1)
	if err != nil {
		return fmt.Errorf("failed to set bIsSCReducedModeEnforcedEx value: %v", err)
	}

	cIPMKey, _, err := registry.CreateKey(registry.LOCAL_MACHINE, cIPMKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open or create cIPM key: %v", err)
	}
	defer cIPMKey.Close()

	err = cIPMKey.SetDWordValue("bDontShowMsgWhenViewingDoc", 0)
	if err != nil {
		return fmt.Errorf("failed to set bDontShowMsgWhenViewingDoc value: %v", err)
	}

	fmt.Println("Registry values set successfully to configure Acrobat in read-only mode.")
	return nil
}

func findUninstallKey(displayName string, uninstallString bool) ([]string, error) {
	var uninstallList []string
	keys := []string{
		`SOFTWARE\Wow6432Node\Microsoft\Windows\CurrentVersion\Uninstall`,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
	}

	for _, key := range keys {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, key, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			return nil, err
		}
		defer k.Close()

		names, err := k.ReadSubKeyNames(-1)
		if err != nil {
			return nil, err
		}

		for _, name := range names {
			subKey, err := registry.OpenKey(k, name, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			defer subKey.Close()

			displayNameValue, _, err := subKey.GetStringValue("DisplayName")
			if err != nil {
				continue
			}

			if strings.Contains(displayNameValue, displayName) {
				if uninstallString {
					uninstallStringValue, _, err := subKey.GetStringValue("UninstallString")
					if err == nil {
						uninstallList = append(uninstallList, uninstallStringValue)
					}
				} else {
					uninstallList = append(uninstallList, displayNameValue)
				}
			}
		}
	}

	return uninstallList, nil
}

func downloadFile(url, path string, attempts int, skipSleep bool) error {
	for i := 0; i < attempts; i++ {
		if !skipSleep && i > 0 {
			sleepTime := time.Duration(3+rand.Intn(12)) * time.Second
			fmt.Printf("Waiting for %v seconds.\n", sleepTime)
			time.Sleep(sleepTime)
		}

		fmt.Printf("Download Attempt %d\n", i+1)
		resp, err := http.Get(url)
		if err != nil {
			fmt.Printf("An error has occurred while downloading: %v\n", err)
			continue
		}
		defer resp.Body.Close()

		out, err := os.Create(path)
		if err != nil {
			return err
		}
		defer out.Close()

		_, err = io.Copy(out, resp.Body)
		if err == nil {
			fmt.Println("Download successful")
			return nil
		} else {
			fmt.Printf("File failed to download: %v\n", err)
		}
	}

	return fmt.Errorf("failed to download file after %d attempts", attempts)
}

func extractZip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", fpath)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}
	return nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-readonlymode" {
		err := setAcrobatReadOnlyMode()
		if err != nil {
			fmt.Printf("Error setting Acrobat to read-only mode: %v\n", err)
			os.Exit(1)
		}
	}

	acrobatProDownloadLocation := filepath.Join(os.TempDir(), "AcrobatPro.zip")
	acrobatProDownloadURL := "https://trials.adobe.com/AdobeProducts/APRO/Acrobat_HelpX/win32/Acrobat_DC_Web_x64_WWMUI.zip"

	uninstallKeys, err := findUninstallKey("Adobe Acrobat", false)
	if err != nil {
		fmt.Printf("Error finding uninstall key: %v\n", err)
		os.Exit(1)
	}

	if len(uninstallKeys) > 0 {
		fmt.Println("Acrobat already installed. Exiting script")
		os.Exit(0)
	}

	fmt.Println("Acrobat install not detected, moving to install step")
	err = downloadFile(acrobatProDownloadURL, acrobatProDownloadLocation, 3, false)
	if err != nil {
		fmt.Printf("Download step failed: %v\n", err)
		os.Exit(1)
	}

	if _, err := os.Stat(acrobatProDownloadLocation); os.IsNotExist(err) {
		fmt.Println("Download file not found")
		os.Exit(1)
	}

	fmt.Println("Extracting archive")
	err = extractZip(acrobatProDownloadLocation, os.TempDir())
	if err != nil {
		fmt.Printf("Could not extract files: %v\n", err)
		os.Exit(1)
	}

	acroProMSI := filepath.Join(os.TempDir(), "Adobe Acrobat", "AcroPro.msi")
	if _, err := os.Stat(acroProMSI); os.IsNotExist(err) {
		fmt.Println("Could not find extracted files")
		os.Exit(1)
	}

	fmt.Println("Waiting 5 seconds")
	time.Sleep(5 * time.Second)

	fmt.Println("Running install step")
	cmd := exec.Command("msiexec.exe", "/i", acroProMSI, "/quiet", "/norestart", "/L*V", filepath.Join(os.TempDir(), "AcrobatProInstall.log"))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	err = cmd.Run()
	if err != nil {
		fmt.Printf("Error running MSI install: %v\n", err)
		os.Exit(1)
	}

	uninstallKeys, err = findUninstallKey("Adobe Acrobat", false)
	if err != nil || len(uninstallKeys) == 0 {
		fmt.Printf("Adobe Acrobat Pro failed to install successfully. Please see the logfile at %s\n", filepath.Join(os.TempDir(), "AcrobatProInstall.log"))
		os.Exit(1)
	}

	fmt.Println("Adobe Acrobat Pro installed successfully.")
	os.RemoveAll(filepath.Join(os.TempDir(), "Adobe Acrobat"))
	os.Remove(acrobatProDownloadLocation)
}
