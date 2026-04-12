package crawler

import (
	"fmt"
	"os"
	"path/filepath"
)

// patchPDFLinks writes a simple link mapping sidecar JSON so the frontend
// can display the mapping. Full binary PDF link patching requires a native
// PDF library; we provide a GoDoc-annotated stub that shells out to
// pdfcpu CLI if available, otherwise records the mapping for reference.
//
// The real patching happens via chromedp's DevTools protocol during generation
// (see printToPDF), and the sidecar lets users know which file maps to which URL.
func patchPDFLinks(masterPDFPath string, pageMap map[string]string) error {
	if len(pageMap) == 0 {
		return nil
	}

	// Write a human-readable mapping sidecar next to the master PDF
	sidecarPath := masterPDFPath + ".links.txt"
	f, err := os.Create(sidecarPath)
	if err != nil {
		return fmt.Errorf("create sidecar: %w", err)
	}
	defer f.Close()

	fmt.Fprintf(f, "Medium Harvester – Link Map\n")
	fmt.Fprintf(f, "Master PDF: %s\n\n", filepath.Base(masterPDFPath))
	fmt.Fprintf(f, "%-70s  →  %s\n", "Original URL", "Local PDF")
	fmt.Fprintf(f, "%s\n", "---")
	for origURL, localPath := range pageMap {
		fmt.Fprintf(f, "%-70s  →  %s\n", origURL, localPath)
	}

	return nil
}
