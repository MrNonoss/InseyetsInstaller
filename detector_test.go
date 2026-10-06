package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultScanDetection(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "inseyets_detect_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create DCode at depth 7: tempDir -> L1 -> L2 -> L3 -> L4 -> L5 -> L6 -> L7
	dcodeFolder := filepath.Join(tempDir, "L1", "L2", "L3", "L4", "L5", "L6", "L7")
	if err := os.MkdirAll(dcodeFolder, 0755); err != nil {
		t.Fatalf("Failed to create DCode dir: %v", err)
	}
	dcodeExe := filepath.Join(dcodeFolder, "DCode-x86-EN-5.7.26188.46.exe")
	_ = os.WriteFile(dcodeExe, []byte("fake-dcode"), 0644)

	// Create PA at depth 4: tempDir -> P1 -> P2 -> P3 -> P4
	paFolder := filepath.Join(tempDir, "P1", "P2", "P3", "P4")
	if err := os.MkdirAll(paFolder, 0755); err != nil {
		t.Fatalf("Failed to create PA dir: %v", err)
	}
	paExe := filepath.Join(paFolder, "Cellebrite_Inseyets_PA_10.11.0.3030.exe")
	_ = os.WriteFile(paExe, []byte("fake-pa"), 0644)
	_ = os.WriteFile(filepath.Join(paFolder, "data.7z"), []byte("fake-7z"), 0644)

	// Create UFED at depth 2: tempDir -> U1 -> U2
	ufedFolder := filepath.Join(tempDir, "U1", "U2")
	if err := os.MkdirAll(ufedFolder, 0755); err != nil {
		t.Fatalf("Failed to create UFED dir: %v", err)
	}
	ufedExe := filepath.Join(ufedFolder, "Cellebrite UFED Setup 10.11.1.457 InseyetsUFED (Fat).exe")
	_ = os.WriteFile(ufedExe, []byte("fake-ufed"), 0644)
	_ = os.WriteFile(filepath.Join(ufedFolder, "data.bin"), []byte("fake-bin"), 0644)

	detected := scanDefaultFolders(tempDir)
	if detected.DCodePath != dcodeExe {
		t.Errorf("Expected DCode path %q, got %q", dcodeExe, detected.DCodePath)
	}
	if detected.PAPath != paExe {
		t.Errorf("Expected PA path %q, got %q", paExe, detected.PAPath)
	}
	if detected.UFEDPath != ufedExe {
		t.Errorf("Expected UFED path %q, got %q", ufedExe, detected.UFEDPath)
	}
}

func TestDCodeDepthLimits(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "dcode_depth_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Depth 8 (exceeds limit 7): tempDir -> 1 -> 2 -> 3 -> 4 -> 5 -> 6 -> 7 -> 8
	deepFolder := filepath.Join(tempDir, "1", "2", "3", "4", "5", "6", "7", "8")
	if err := os.MkdirAll(deepFolder, 0755); err != nil {
		t.Fatalf("Failed to create depth 8 dir: %v", err)
	}
	deepExe := filepath.Join(deepFolder, "DCode-x86-EN-5.7.26188.46.exe")
	_ = os.WriteFile(deepExe, []byte("fake-dcode"), 0644)

	detected := scanDefaultFolders(tempDir)
	if detected.DCodePath != "" {
		t.Errorf("Expected DCode at depth 8 not to be detected, got %q", detected.DCodePath)
	}
}

func TestPADepthLimits(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pa_depth_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Depth 5 (exceeds default limit 4): tempDir -> 1 -> 2 -> 3 -> 4 -> 5
	deepPA := filepath.Join(tempDir, "1", "2", "3", "4", "5")
	if err := os.MkdirAll(deepPA, 0755); err != nil {
		t.Fatalf("Failed to create depth 5 dir: %v", err)
	}
	_ = os.WriteFile(filepath.Join(deepPA, "Cellebrite_Inseyets_PA_10.11.0.3030.exe"), []byte("fake"), 0644)
	_ = os.WriteFile(filepath.Join(deepPA, "data.7z"), []byte("fake"), 0644)

	detected := scanDefaultFolders(tempDir)
	if detected.PAPath != "" {
		t.Errorf("Expected PA at depth 5 not to be detected by default scan, got %q", detected.PAPath)
	}
}

func TestMultipleVersionsPicksLatest(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "inseyets_ver_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Older DCode
	oldDCode := filepath.Join(tempDir, "DCode_Old")
	_ = os.MkdirAll(oldDCode, 0755)
	_ = os.WriteFile(filepath.Join(oldDCode, "DCode-x86-EN-5.6.1000.exe"), []byte("fake"), 0644)

	// Newer DCode
	newDCode := filepath.Join(tempDir, "DCode_New")
	_ = os.MkdirAll(newDCode, 0755)
	expectedDCode := filepath.Join(newDCode, "DCode-x86-EN-5.7.26188.46.exe")
	_ = os.WriteFile(expectedDCode, []byte("fake"), 0644)

	// Older PA
	oldPA := filepath.Join(tempDir, "Cellebrite_Inseyets_PA_10.10.0.1000_20260101")
	_ = os.MkdirAll(oldPA, 0755)
	_ = os.WriteFile(filepath.Join(oldPA, "Cellebrite_Inseyets_PA_10.10.0.1000.exe"), []byte("fake"), 0644)
	_ = os.WriteFile(filepath.Join(oldPA, "data.7z"), []byte("fake"), 0644)

	// Newer PA
	newPA := filepath.Join(tempDir, "Cellebrite_Inseyets_PA_10.11.0.3030_202609291243")
	_ = os.MkdirAll(newPA, 0755)
	expectedPA := filepath.Join(newPA, "Cellebrite_Inseyets_PA_10.11.0.3030.exe")
	_ = os.WriteFile(expectedPA, []byte("fake"), 0644)
	_ = os.WriteFile(filepath.Join(newPA, "data.7z"), []byte("fake"), 0644)

	detected := scanDefaultFolders(tempDir)
	if detected.DCodePath != expectedDCode {
		t.Errorf("Expected latest DCode %q, got %q", expectedDCode, detected.DCodePath)
	}
	if detected.PAPath != expectedPA {
		t.Errorf("Expected latest PA %q, got %q", expectedPA, detected.PAPath)
	}
}

func TestConstructDCodeArgs(t *testing.T) {
	expected := []string{"/SP-", "/VERYSILENT", "/SUPPRESSMSGBOXES"}
	got := constructDCodeArgs("C:\\test\\DCode-x86-EN-5.7.26188.46.exe")
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("Expected %v, got %v", expected, got)
	}
}

func TestParentDirectoryScan(t *testing.T) {
	tempParent, err := os.MkdirTemp("", "inseyets_parent_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp parent: %v", err)
	}
	defer os.RemoveAll(tempParent)

	// Tool directory inside parent
	toolDir := filepath.Join(tempParent, "Inseyets_Installer")
	if err := os.MkdirAll(toolDir, 0755); err != nil {
		t.Fatalf("Failed to create toolDir: %v", err)
	}

	// 1. DCode directly in parent directory
	parentDCode := filepath.Join(tempParent, "DCode-x86-EN-5.7.26188.46.exe")
	_ = os.WriteFile(parentDCode, []byte("fake-dcode"), 0644)

	// 2. PA in sibling directory under parent
	siblingPA := filepath.Join(tempParent, "Sibling_PA")
	_ = os.MkdirAll(siblingPA, 0755)
	siblingPAExe := filepath.Join(siblingPA, "Cellebrite_Inseyets_PA_10.11.0.3030.exe")
	_ = os.WriteFile(siblingPAExe, []byte("fake-pa"), 0644)
	_ = os.WriteFile(filepath.Join(siblingPA, "data.7z"), []byte("fake-7z"), 0644)

	// 3. UFED inside toolDir's own subfolder
	toolUFED := filepath.Join(toolDir, "Local_UFED")
	_ = os.MkdirAll(toolUFED, 0755)
	toolUFEDExe := filepath.Join(toolUFED, "Cellebrite UFED Setup 10.11.1.457 InseyetsUFED (Fat).exe")
	_ = os.WriteFile(toolUFEDExe, []byte("fake-ufed"), 0644)
	_ = os.WriteFile(filepath.Join(toolUFED, "data.bin"), []byte("fake-bin"), 0644)

	// Scan from toolDir
	detected := scanDefaultFolders(toolDir)

	if detected.DCodePath != parentDCode {
		t.Errorf("Expected DCode from parent dir %q, got %q", parentDCode, detected.DCodePath)
	}
	if detected.PAPath != siblingPAExe {
		t.Errorf("Expected PA from sibling dir %q, got %q", siblingPAExe, detected.PAPath)
	}
	if detected.UFEDPath != toolUFEDExe {
		t.Errorf("Expected UFED from toolDir subfolder %q, got %q", toolUFEDExe, detected.UFEDPath)
	}
}

func TestFalsePositiveExclusions(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "inseyets_fp_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Folder containing PasswordCombinationTool and a random .7z file (simulating Downloads)
	dlFolder := filepath.Join(tempDir, "Downloads")
	_ = os.MkdirAll(dlFolder, 0755)
	_ = os.WriteFile(filepath.Join(dlFolder, "PasswordCombinationTool-v2.2-Portable-x64.exe"), []byte("fake"), 0644)
	_ = os.WriteFile(filepath.Join(dlFolder, "some_archive.7z"), []byte("fake-7z"), 0644)

	// Folder containing UFEDReader.exe
	readerFolder := filepath.Join(tempDir, "UFED_Reader_Tool")
	_ = os.MkdirAll(readerFolder, 0755)
	_ = os.WriteFile(filepath.Join(readerFolder, "UFEDReader.exe"), []byte("fake-reader"), 0644)

	detected := scanDefaultFolders(tempDir)

	if detected.PAPath != "" {
		t.Errorf("PasswordCombinationTool was falsely identified as PA: %q", detected.PAPath)
	}
	if detected.UFEDPath != "" {
		t.Errorf("UFEDReader was falsely identified as UFED: %q", detected.UFEDPath)
	}
}

func TestAppDataExcluded(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "inseyets_appdata_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Put an installer inside an AppData folder
	appDataFolder := filepath.Join(tempDir, "AppData", "Local", "Temp")
	_ = os.MkdirAll(appDataFolder, 0755)
	_ = os.WriteFile(filepath.Join(appDataFolder, "DCode-x86-EN-5.7.26188.46.exe"), []byte("fake"), 0644)

	detected := scanDefaultFolders(tempDir)
	if detected.DCodePath != "" {
		t.Errorf("Installer inside AppData should be skipped, but got: %q", detected.DCodePath)
	}
}


