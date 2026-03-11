package service

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Sentinel errors for PDF operations.
var (
	ErrInvalidPassword = errors.New("invalid password")
	ErrNotEncrypted    = errors.New("pdf is not password-protected")
)

// PDFService handles PDF encryption and decryption operations using qpdf.
type PDFService struct {
	uploadDir    string
	generatedDir string
}

// NewPDFService creates a new PDF service.
func NewPDFService(uploadDir, generatedDir string) *PDFService {
	return &PDFService{
		uploadDir:    uploadDir,
		generatedDir: generatedDir,
	}
}

// CheckQPDF verifies that qpdf is installed and available on PATH.
func (s *PDFService) CheckQPDF() error {
	if _, err := exec.LookPath("qpdf"); err != nil {
		return fmt.Errorf("qpdf not found on PATH — install it: https://github.com/qpdf/qpdf/releases")
	}
	return nil
}

// IsEncrypted checks whether a PDF file is password-protected.
func (s *PDFService) IsEncrypted(inputPath string) (bool, error) {
	cmd := exec.Command("qpdf", "--is-encrypted", inputPath)
	err := cmd.Run()
	if err == nil {
		// exit code 0 = encrypted
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
		// exit code 2 = not encrypted
		return false, nil
	}
	return false, fmt.Errorf("check encryption: %w", err)
}

// LockPDF encrypts a PDF file with the given password using qpdf (AES-256).
// Returns the path to the encrypted output file.
func (s *PDFService) LockPDF(inputPath, outputFilename, userPassword, ownerPassword string) (string, error) {
	outputPath := filepath.Join(s.generatedDir, outputFilename)

	if ownerPassword == "" {
		ownerPassword = userPassword
	}

	// qpdf --encrypt <user-pw> <owner-pw> <key-length> -- input.pdf output.pdf
	args := []string{
		"--encrypt", userPassword, ownerPassword, "256",
		"--", inputPath, outputPath,
	}

	if err := runQPDF(args); err != nil {
		return "", fmt.Errorf("encrypt pdf: %w", err)
	}

	slog.Info("pdf locked successfully", "input", filepath.Base(inputPath), "output", outputFilename)
	return outputPath, nil
}

// UnlockPDF decrypts a password-protected PDF file using qpdf.
// Returns the path to the decrypted output file.
func (s *PDFService) UnlockPDF(inputPath, outputFilename, password string) (string, error) {
	outputPath := filepath.Join(s.generatedDir, outputFilename)

	// qpdf --decrypt --password=<pw> input.pdf output.pdf
	args := []string{
		"--decrypt",
		"--password=" + password,
		inputPath, outputPath,
	}

	if err := runQPDF(args); err != nil {
		if isInvalidPasswordError(err) {
			return "", ErrInvalidPassword
		}
		return "", fmt.Errorf("decrypt pdf: %w", err)
	}

	slog.Info("pdf unlocked successfully", "input", filepath.Base(inputPath), "output", outputFilename)
	return outputPath, nil
}

// EnsureDirectories creates the upload and generated directories if they don't exist.
func (s *PDFService) EnsureDirectories() error {
	for _, dir := range []string{s.uploadDir, s.generatedDir} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	return nil
}

// CheckGhostscript verifies that Ghostscript is installed and available on PATH.
func (s *PDFService) CheckGhostscript() error {
	if _, err := findGhostscript(); err != nil {
		return fmt.Errorf("ghostscript not found on PATH — install it: https://ghostscript.com/releases/")
	}
	return nil
}

// CompressionResult holds stats from a compression operation.
type CompressionResult struct {
	OutputPath     string
	OriginalSize   int64
	CompressedSize int64
}

// CompressPDF compresses a PDF using Ghostscript with the given quality level.
// level must be one of: "low", "medium", "high", "maximum".
// Returns the output path and compression stats.
func (s *PDFService) CompressPDF(inputPath, outputFilename, level string) (*CompressionResult, error) {
	outputPath := filepath.Join(s.generatedDir, outputFilename)

	gsPath, err := findGhostscript()
	if err != nil {
		return nil, fmt.Errorf("ghostscript not available: %w", err)
	}

	info, err := os.Stat(inputPath)
	if err != nil {
		return nil, fmt.Errorf("stat input file: %w", err)
	}
	originalSize := info.Size()

	args := buildGhostscriptArgs(inputPath, outputPath, level)

	cmd := exec.Command(gsPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ghostscript compression failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	outInfo, err := os.Stat(outputPath)
	if err != nil {
		return nil, fmt.Errorf("stat output file: %w", err)
	}

	// If compressed is larger than original, copy original as output
	if outInfo.Size() >= originalSize {
		slog.Info("compression did not reduce size, using original", "input", filepath.Base(inputPath))
		data, err := os.ReadFile(inputPath)
		if err != nil {
			return nil, fmt.Errorf("read original for fallback: %w", err)
		}
		if err := os.WriteFile(outputPath, data, 0600); err != nil {
			return nil, fmt.Errorf("write fallback output: %w", err)
		}
		outInfo, _ = os.Stat(outputPath)
	}

	slog.Info("pdf compressed successfully",
		"input", filepath.Base(inputPath),
		"output", outputFilename,
		"level", level,
		"original_bytes", originalSize,
		"compressed_bytes", outInfo.Size(),
	)

	return &CompressionResult{
		OutputPath:     outputPath,
		OriginalSize:   originalSize,
		CompressedSize: outInfo.Size(),
	}, nil
}

// GetPageCount returns the number of pages in a PDF file.
func (s *PDFService) GetPageCount(inputPath string) (int, error) {
	cmd := exec.Command("qpdf", "--show-npages", inputPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("get page count: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	n, err := strconv.Atoi(strings.TrimSpace(stdout.String()))
	if err != nil {
		return 0, fmt.Errorf("parse page count: %w", err)
	}
	return n, nil
}

// MergePDFs combines multiple PDF files into a single output PDF using qpdf.
// inputPaths are merged in the order provided. Returns the path to the merged output file.
func (s *PDFService) MergePDFs(inputPaths []string, outputFilename string) (string, error) {
	if len(inputPaths) < 2 {
		return "", fmt.Errorf("at least 2 PDF files are required for merging")
	}

	outputPath := filepath.Join(s.generatedDir, outputFilename)

	// qpdf --empty --pages file1.pdf file2.pdf ... -- output.pdf
	args := []string{"--empty", "--pages"}
	args = append(args, inputPaths...)
	args = append(args, "--", outputPath)

	if err := runQPDF(args); err != nil {
		return "", fmt.Errorf("merge pdfs: %w", err)
	}

	slog.Info("pdfs merged successfully", "file_count", len(inputPaths), "output", outputFilename)
	return outputPath, nil
}

// SplitResult holds the output from a split operation.
type SplitResult struct {
	ZipPath    string
	PageCount  int
	SplitCount int
}

// SplitPDF splits a PDF into multiple files and packages the results into a ZIP archive.
// mode "all" splits every page; mode "ranges" splits by the comma-separated page ranges in pageRanges.
// Returns the path to the ZIP file, total page count, and split count.
func (s *PDFService) SplitPDF(inputPath, outputPrefix, mode, pageRanges string) (*SplitResult, error) {
	pageCount, err := s.GetPageCount(inputPath)
	if err != nil {
		return nil, fmt.Errorf("get page count for split: %w", err)
	}
	if pageCount < 2 {
		return nil, fmt.Errorf("PDF has only %d page — splitting requires at least 2 pages", pageCount)
	}

	var splitFiles []string

	switch mode {
	case "all":
		splitFiles, err = s.splitAllPages(inputPath, outputPrefix, pageCount)
	case "ranges":
		splitFiles, err = s.splitByRanges(inputPath, outputPrefix, pageRanges, pageCount)
	default:
		return nil, fmt.Errorf("invalid split mode %q — use \"all\" or \"ranges\"", mode)
	}
	if err != nil {
		return nil, err
	}

	// Package split PDFs into a ZIP archive
	zipPath := filepath.Join(s.generatedDir, outputPrefix+".zip")
	if err := createZipFromFiles(zipPath, splitFiles); err != nil {
		cleanupFiles(splitFiles)
		return nil, fmt.Errorf("create zip archive: %w", err)
	}

	// Remove intermediate split PDFs (they're in the ZIP now)
	cleanupFiles(splitFiles)

	slog.Info("pdf split successfully",
		"input", filepath.Base(inputPath),
		"mode", mode,
		"page_count", pageCount,
		"split_count", len(splitFiles),
	)

	return &SplitResult{
		ZipPath:    zipPath,
		PageCount:  pageCount,
		SplitCount: len(splitFiles),
	}, nil
}

// splitAllPages extracts each page of the PDF into its own file.
func (s *PDFService) splitAllPages(inputPath, outputPrefix string, pageCount int) ([]string, error) {
	var files []string
	for i := 1; i <= pageCount; i++ {
		outName := fmt.Sprintf("%s_page_%d.pdf", outputPrefix, i)
		outPath := filepath.Join(s.generatedDir, outName)
		pageRange := fmt.Sprintf("%d-%d", i, i)

		args := []string{inputPath, "--pages", inputPath, pageRange, "--", outPath}
		if err := runQPDF(args); err != nil {
			cleanupFiles(files)
			return nil, fmt.Errorf("split page %d: %w", i, err)
		}
		files = append(files, outPath)
	}
	return files, nil
}

// splitByRanges extracts user-specified page ranges into separate PDFs.
// pageRanges format: "1-3,4-6,7-10" — each comma-separated range becomes one PDF.
func (s *PDFService) splitByRanges(inputPath, outputPrefix, pageRanges string, pageCount int) ([]string, error) {
	ranges, err := parsePageRanges(pageRanges, pageCount)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, pr := range ranges {
		outName := fmt.Sprintf("%s_pages_%d-%d.pdf", outputPrefix, pr.start, pr.end)
		outPath := filepath.Join(s.generatedDir, outName)
		rangeStr := fmt.Sprintf("%d-%d", pr.start, pr.end)

		args := []string{inputPath, "--pages", inputPath, rangeStr, "--", outPath}
		if err := runQPDF(args); err != nil {
			cleanupFiles(files)
			return nil, fmt.Errorf("split range %s: %w", rangeStr, err)
		}
		files = append(files, outPath)
	}
	return files, nil
}

type pageRange struct {
	start, end int
}

// pageRangePattern matches "N" or "N-M".
var pageRangePattern = regexp.MustCompile(`^(\d+)(?:-(\d+))?$`)

// parsePageRanges parses and validates a comma-separated page range string like "1-3,4-6,7-10".
func parsePageRanges(raw string, pageCount int) ([]pageRange, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("page ranges cannot be empty when mode is \"ranges\"")
	}

	parts := strings.Split(raw, ",")
	var ranges []pageRange

	for _, p := range parts {
		p = strings.TrimSpace(p)
		matches := pageRangePattern.FindStringSubmatch(p)
		if matches == nil {
			return nil, fmt.Errorf("invalid page range %q — expected format like \"1-3\" or \"5\"", p)
		}

		start, _ := strconv.Atoi(matches[1])
		end := start
		if matches[2] != "" {
			end, _ = strconv.Atoi(matches[2])
		}

		if start < 1 || end < 1 {
			return nil, fmt.Errorf("page numbers must be positive, got %q", p)
		}
		if start > end {
			return nil, fmt.Errorf("invalid range %q — start page cannot be greater than end page", p)
		}
		if end > pageCount {
			return nil, fmt.Errorf("page %d exceeds document page count (%d)", end, pageCount)
		}

		ranges = append(ranges, pageRange{start: start, end: end})
	}

	if len(ranges) == 0 {
		return nil, fmt.Errorf("no valid page ranges provided")
	}

	return ranges, nil
}

// createZipFromFiles creates a ZIP archive containing the given files (using their base names).
func createZipFromFiles(zipPath string, files []string) error {
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("create zip file: %w", err)
	}
	defer zipFile.Close()

	w := zip.NewWriter(zipFile)
	defer w.Close()

	// Sort files to ensure deterministic ZIP ordering
	sorted := make([]string, len(files))
	copy(sorted, files)
	sort.Strings(sorted)

	for _, fpath := range sorted {
		f, err := os.Open(fpath)
		if err != nil {
			return fmt.Errorf("open file for zip: %w", err)
		}

		info, err := f.Stat()
		if err != nil {
			f.Close()
			return fmt.Errorf("stat file for zip: %w", err)
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			f.Close()
			return fmt.Errorf("create zip header: %w", err)
		}
		header.Name = filepath.Base(fpath)
		header.Method = zip.Deflate

		writer, err := w.CreateHeader(header)
		if err != nil {
			f.Close()
			return fmt.Errorf("create zip entry: %w", err)
		}

		if _, err := io.Copy(writer, f); err != nil {
			f.Close()
			return fmt.Errorf("write zip entry: %w", err)
		}
		f.Close()
	}

	return nil
}

// cleanupFiles removes a list of files from disk, logging warnings on failure.
func cleanupFiles(files []string) {
	for _, f := range files {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			slog.Warn("cleanup: failed to remove intermediate file", "path", f, "error", err)
		}
	}
}

// buildGhostscriptArgs constructs optimized Ghostscript arguments for each compression level.
func buildGhostscriptArgs(inputPath, outputPath, level string) []string {
	// Base args common to all levels
	args := []string{
		"-sDEVICE=pdfwrite",
		"-dCompatibilityLevel=1.5",
		"-dNOPAUSE",
		"-dBATCH",
		"-dQUIET",
		"-sOutputFile=" + outputPath,
		// Optimize for size
		"-dDetectDuplicateImages=true",
		"-dCompressFonts=true",
		"-dSubsetFonts=true",
		"-dConvertCMYKImagesToRGB=true",
		"-dAutoFilterColorImages=false",
		"-dAutoFilterGrayImages=false",
	}

	switch level {
	case "low":
		// Light compression — 300 DPI, high JPEG quality
		args = append(args,
			"-dPDFSETTINGS=/printer",
			"-dColorImageDownsampleType=/Bicubic",
			"-dColorImageResolution=300",
			"-dGrayImageDownsampleType=/Bicubic",
			"-dGrayImageResolution=300",
			"-dMonoImageDownsampleType=/Bicubic",
			"-dMonoImageResolution=300",
			"-dColorImageFilter=/DCTEncode",
			"-dGrayImageFilter=/DCTEncode",
			"-c", "<< /ColorACSImageDict << /QFactor 0.4 /Blend 1 /HSamples [1 1 1 1] /VSamples [1 1 1 1] >> /GrayACSImageDict << /QFactor 0.4 /Blend 1 /HSamples [1 1 1 1] /VSamples [1 1 1 1] >> >> setdistillerparams",
			"-f",
		)
	case "medium":
		// Balanced — 150 DPI, moderate JPEG quality
		args = append(args,
			"-dPDFSETTINGS=/ebook",
			"-dColorImageDownsampleType=/Bicubic",
			"-dColorImageResolution=150",
			"-dGrayImageDownsampleType=/Bicubic",
			"-dGrayImageResolution=150",
			"-dMonoImageDownsampleType=/Bicubic",
			"-dMonoImageResolution=150",
			"-dColorImageFilter=/DCTEncode",
			"-dGrayImageFilter=/DCTEncode",
			"-c", "<< /ColorACSImageDict << /QFactor 0.76 /Blend 1 /HSamples [2 1 1 2] /VSamples [2 1 1 2] >> /GrayACSImageDict << /QFactor 0.76 /Blend 1 /HSamples [2 1 1 2] /VSamples [2 1 1 2] >> >> setdistillerparams",
			"-f",
		)
	case "high":
		// Aggressive — 96 DPI, higher JPEG compression
		args = append(args,
			"-dPDFSETTINGS=/ebook",
			"-dColorImageDownsampleType=/Bicubic",
			"-dColorImageResolution=96",
			"-dGrayImageDownsampleType=/Bicubic",
			"-dGrayImageResolution=96",
			"-dMonoImageDownsampleType=/Bicubic",
			"-dMonoImageResolution=96",
			"-dColorImageFilter=/DCTEncode",
			"-dGrayImageFilter=/DCTEncode",
			"-c", "<< /ColorACSImageDict << /QFactor 1.5 /Blend 1 /HSamples [2 1 1 2] /VSamples [2 1 1 2] >> /GrayACSImageDict << /QFactor 1.5 /Blend 1 /HSamples [2 1 1 2] /VSamples [2 1 1 2] >> >> setdistillerparams",
			"-f",
		)
	case "maximum":
		// Extreme — 72 DPI, maximum JPEG compression
		args = append(args,
			"-dPDFSETTINGS=/screen",
			"-dColorImageDownsampleType=/Bicubic",
			"-dColorImageResolution=72",
			"-dGrayImageDownsampleType=/Bicubic",
			"-dGrayImageResolution=72",
			"-dMonoImageDownsampleType=/Bicubic",
			"-dMonoImageResolution=72",
			"-dColorImageFilter=/DCTEncode",
			"-dGrayImageFilter=/DCTEncode",
			"-c", "<< /ColorACSImageDict << /QFactor 2.4 /Blend 1 /HSamples [2 1 1 2] /VSamples [2 1 1 2] >> /GrayACSImageDict << /QFactor 2.4 /Blend 1 /HSamples [2 1 1 2] /VSamples [2 1 1 2] >> >> setdistillerparams",
			"-f",
		)
	default:
		// Default to medium
		args = append(args, "-dPDFSETTINGS=/ebook")
	}

	// Input file always last
	args = append(args, inputPath)

	return args
}

// findGhostscript locates the Ghostscript executable across platforms.
func findGhostscript() (string, error) {
	names := []string{"gs"}
	if runtime.GOOS == "windows" {
		names = []string{"gswin64c", "gswin32c", "gs"}
	}

	// 1. Try PATH first.
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}

	// 2. On Windows, check common install directories.
	if runtime.GOOS == "windows" {
		for _, root := range []string{
			os.Getenv("ProgramFiles"),
			os.Getenv("ProgramFiles(x86)"),
		} {
			if root == "" {
				continue
			}
			gsRoot := filepath.Join(root, "gs")
			entries, err := os.ReadDir(gsRoot)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				for _, name := range names {
					candidate := filepath.Join(gsRoot, e.Name(), "bin", name+".exe")
					if _, err := os.Stat(candidate); err == nil {
						return candidate, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("ghostscript not found (tried PATH and common install directories)")
}

// runQPDF executes a qpdf command and returns any errors with stderr output.
func runQPDF(args []string) error {
	cmd := exec.Command("qpdf", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// isInvalidPasswordError checks if the qpdf error indicates a wrong password.
func isInvalidPasswordError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "invalid password")
}
