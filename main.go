package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const owner = "AwesomeGCP.com"

var errSkip = errors.New("skip")

type Config struct {
	replace   bool
	recursive bool
	ext       string
	parallel  int
	threshold float64
	minQ      int
	maxQ      int
	step      int
}

type Result struct {
	input, output string
	before, after int64
	quality       int
	ssim          float64
	converted     bool
	err           error
	duration      time.Duration
}

var imageExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".webp": true,
}

func isImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return imageExtensions[ext]
}

func collectInputs(args []string, recursive bool) ([]string, error) {
	var inputs []string
	for _, arg := range args {
		st, err := os.Stat(arg)
		if err != nil {
			inputs = append(inputs, arg)
			continue
		}
		if st.IsDir() {
			if !recursive {
				inputs = append(inputs, arg)
				continue
			}
			err := filepath.WalkDir(arg, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() && isImageFile(path) {
					inputs = append(inputs, path)
				}
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("failed to scan directory %s: %w", arg, err)
			}
		} else {
			inputs = append(inputs, arg)
		}
	}
	return inputs, nil
}

func main() {
	var cfg Config
	flag.BoolVar(&cfg.replace, "replace", false, "replace input files in place")
	flag.BoolVar(&cfg.recursive, "recursive", false, "recursively search for image files in directories")
	flag.BoolVar(&cfg.recursive, "r", false, "recursively search for image files in directories (shorthand)")
	flag.StringVar(&cfg.ext, "ext", "", "output extension, e.g. jpg, webp, png; defaults to source extension")
	flag.IntVar(&cfg.parallel, "parallel", runtime.NumCPU(), "number of images to process concurrently")
	flag.Float64Var(&cfg.threshold, "quality", 0.98, "minimum SSIM for lossy conversions, from 0 to 1")
	flag.IntVar(&cfg.minQ, "min-quality", 60, "minimum encoder quality considered")
	flag.IntVar(&cfg.maxQ, "max-quality", 95, "maximum encoder quality considered")
	flag.IntVar(&cfg.step, "quality-step", 5, "quality search step")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() == 0 {
		usage()
		os.Exit(2)
	}
	if cfg.parallel < 1 || cfg.threshold <= 0 || cfg.threshold > 1 || cfg.minQ < 1 || cfg.maxQ > 100 || cfg.minQ > cfg.maxQ || cfg.step < 1 {
		fmt.Fprintln(os.Stderr, "invalid options")
		os.Exit(2)
	}
	if cfg.replace && cfg.ext != "" {
		fmt.Fprintln(os.Stderr, "-replace cannot be combined with -ext because replacement must keep the source path")
		os.Exit(2)
	}

	inputs, err := collectInputs(flag.Args(), cfg.recursive)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(inputs) == 0 {
		fmt.Println("No matching image files found.")
		return
	}

	checkDependencies(cfg)

	total := len(inputs)
	var (
		mu         sync.Mutex
		inProgress int
		completed  int
	)

	updateProgress := func() {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(os.Stderr, "\rProgress: %d found | %d in progress | %d/%d completed", total, inProgress, completed, total)
	}

	updateProgress()

	jobs := make(chan string)
	results := make(chan Result)
	var wg sync.WaitGroup
	for i := 0; i < cfg.parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for input := range jobs {
				mu.Lock()
				inProgress++
				mu.Unlock()
				updateProgress()

				res := process(input, cfg)

				mu.Lock()
				inProgress--
				completed++
				mu.Unlock()
				updateProgress()

				results <- res
			}
		}()
	}
	go func() {
		for _, input := range inputs {
			jobs <- input
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	var all []Result
	for r := range results {
		all = append(all, r)
	}

	// Clear the progress indicator line before printing final summary table
	fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", 80))

	var totalBefore, totalAfter int64
	fmt.Println()
	fmt.Printf("%-32s %10s %10s %9s %8s\n", "FILE", "BEFORE", "AFTER", "CHANGE", "SSIM")
	fmt.Println(strings.Repeat("-", 76))
	for _, r := range all {
		name := filepath.Base(r.input)
		if r.err != nil {
			if errors.Is(r.err, errSkip) {
				fmt.Printf("%-32s SKIPPED (optimized file was not smaller)\n", truncate(name, 32))
			} else {
				fmt.Printf("%-32s ERROR: %v\n", truncate(name, 32), r.err)
			}
			continue
		}
		totalBefore += r.before
		totalAfter += r.after
		change := 0.0
		if r.before > 0 {
			change = (1 - float64(r.after)/float64(r.before)) * 100
		}
		ssim := "-"
		if r.ssim > 0 {
			ssim = fmt.Sprintf("%.4f", r.ssim)
		}
		fmt.Printf("%-32s %10s %10s %8.1f%% %8s\n", truncate(name, 32), human(r.before), human(r.after), change, ssim)
	}
	if totalBefore > 0 {
		fmt.Println(strings.Repeat("-", 76))
		change := (1 - float64(totalAfter)/float64(totalBefore)) * 100
		fmt.Printf("%-32s %10s %10s %8.1f%%\n", "TOTAL", human(totalBefore), human(totalAfter), change)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `tinyimg - local image optimizer

Usage:
  tinyimg [options] image1 image2 ...
  tinyimg [options] -recursive dir1 dir2 ...

Options:
  -recursive, -r       search for image files recursively in directories
  -replace             replace each input file in place
  -ext string          output extension (jpg, webp, png, jpeg); default: source extension
  -parallel int        concurrent jobs (default: CPU count)
  -quality float       minimum SSIM for lossy output (default: 0.98)
  -min-quality int     minimum encoder quality (default: 60)
  -max-quality int     maximum encoder quality (default: 95)
  -quality-step int    quality search step (default: 5)

Examples:
  tinyimg *.png
  tinyimg -recursive ./photos
  tinyimg -ext jpg *.png
  tinyimg -replace *.jpg
  tinyimg -ext webp -quality 0.985 *.png

Notes:
  * Metadata is stripped and Owner is set to %s.
  * Lossy conversions use ImageMagick SSIM to find the smallest acceptable output.
  * PNG optimization is lossless.
`, owner)
}

func checkDependencies(cfg Config) {
	deps := []string{"magick", "exiftool"}
	if cfg.ext == "" {
		// PNG and JPEG can still need magick; oxipng is used when available.
		deps = append(deps, "oxipng")
	}
	for _, d := range deps {
		if _, err := exec.LookPath(d); err != nil {
			fmt.Fprintf(os.Stderr, "missing dependency: %s\n", d)
			fmt.Fprintln(os.Stderr, "Install with: brew install imagemagick exiftool oxipng")
			os.Exit(2)
		}
	}
}

func process(input string, cfg Config) Result {
	start := time.Now()
	r := Result{input: input, output: input, quality: 0}
	st, err := os.Stat(input)
	if err != nil {
		r.err = err
		return r
	}
	if st.IsDir() {
		r.err = fmt.Errorf("is a directory")
		return r
	}
	r.before = st.Size()

	srcExt := strings.TrimPrefix(strings.ToLower(filepath.Ext(input)), ".")
	targetExt := srcExt
	if cfg.ext != "" {
		targetExt = normalizeExt(cfg.ext)
	}
	if targetExt == "jpeg" {
		targetExt = "jpg"
	}
	if targetExt != "png" && targetExt != "jpg" && targetExt != "webp" {
		r.err = fmt.Errorf("unsupported output extension: %s", targetExt)
		return r
	}

	output := input
	if !cfg.replace {
		if cfg.ext != "" {
			output = strings.TrimSuffix(input, filepath.Ext(input)) + "." + targetExt
		} else {
			output = input + ".optimized" + filepath.Ext(input)
		}
	}
	r.output = output

	tmp, err := os.CreateTemp(filepath.Dir(input), ".tinyimg-*")
	if err != nil {
		r.err = err
		return r
	}
	tmpPath := tmp.Name()
	tmp.Close()
	os.Remove(tmpPath)
	defer os.Remove(tmpPath)

	var ssim float64
	var quality int
	if targetExt == "png" {
		err = optimizePNG(input, tmpPath)
	} else {
		quality, ssim, err = optimizeLossy(input, tmpPath, targetExt, cfg)
	}
	if err != nil {
		r.err = err
		return r
	}

	if err = stripAndSetOwner(tmpPath); err != nil {
		r.err = err
		return r
	}

	outSt, err := os.Stat(tmpPath)
	if err != nil {
		r.err = err
		return r
	}
	if outSt.Size() >= r.before && sameExt(input, output) {
		// Never replace a file with a larger version when no format conversion was requested.
		if cfg.replace {
			r.err = errSkip
			return r
		}
		if err := copyFile(input, output); err != nil {
			r.err = err
			return r
		}
		r.after = r.before
		r.ssim = 1
		r.duration = time.Since(start)
		return r
	}

	if cfg.replace {
		backup := input + ".tinyimg-backup"
		if err := os.Rename(input, backup); err != nil {
			r.err = err
			return r
		}
		if err := os.Rename(tmpPath, input); err != nil {
			_ = os.Rename(backup, input)
			r.err = err
			return r
		}
		_ = os.Remove(backup)
	} else {
		if err := os.Rename(tmpPath, output); err != nil {
			r.err = err
			return r
		}
	}

	st, err = os.Stat(output)
	if cfg.replace {
		st, err = os.Stat(input)
	}
	if err != nil {
		r.err = err
		return r
	}
	r.after = st.Size()
	r.quality = quality
	r.ssim = ssim
	r.converted = targetExt != srcExt
	r.duration = time.Since(start)
	return r
}

func optimizePNG(input, output string) error {
	cmd := exec.Command("oxipng", "-o", "4", "--strip", "safe", "-out", output, input)
	return run(cmd)
}

func optimizeLossy(input, output, ext string, cfg Config) (int, float64, error) {
	qualities := make([]int, 0, (cfg.maxQ-cfg.minQ)/cfg.step+2)
	for q := cfg.minQ; q <= cfg.maxQ; q += cfg.step {
		qualities = append(qualities, q)
	}
	if qualities[len(qualities)-1] != cfg.maxQ {
		qualities = append(qualities, cfg.maxQ)
	}

	bestQ := 0
	bestSSIM := 0.0
	bestSize := int64(1<<62 - 1)
	bestPath := output + ".best"
	defer os.Remove(bestPath)

	for _, q := range qualities {
		candidate := output + fmt.Sprintf(".%d", q)
		os.Remove(candidate)
		args := []string{"-quiet", input}
		if ext == "jpg" {
			args = append(args, "-background", "white", "-alpha", "remove", "-alpha", "off")
		}
		args = append(args, "-strip", "-quality", strconv.Itoa(q), candidate)
		if err := run(exec.Command("magick", args...)); err != nil {
			return 0, 0, err
		}
		ssim, err := measureSSIM(input, candidate)
		if err != nil {
			return 0, 0, err
		}
		st, err := os.Stat(candidate)
		if err != nil {
			return 0, 0, err
		}
		if ssim >= cfg.threshold && st.Size() < bestSize {
			bestSize = st.Size()
			bestQ = q
			bestSSIM = ssim
			if err := copyFile(candidate, bestPath); err != nil {
				return 0, 0, err
			}
		}
		os.Remove(candidate)
	}
	if bestQ == 0 {
		return 0, 0, fmt.Errorf("could not meet SSIM threshold %.4f", cfg.threshold)
	}
	if err := os.Rename(bestPath, output); err != nil {
		return 0, 0, err
	}
	return bestQ, bestSSIM, nil
}

func measureSSIM(original, candidate string) (float64, error) {
	cmd := exec.Command("magick", "compare", "-metric", "SSIM", original, candidate, "null:")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	_ = cmd.Run() // compare returns non-zero when images differ.
	line := strings.TrimSpace(stderr.String())
	if line == "" {
		return 0, fmt.Errorf("could not measure SSIM")
	}
	// ImageMagick commonly returns: "0.987654 (0.012346)"
	fields := strings.Fields(line)
	v, err := strconv.ParseFloat(strings.TrimSpace(fields[0]), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid SSIM output %q: %w", line, err)
	}
	return v, nil
}

func stripAndSetOwner(path string) error {
	return run(exec.Command("exiftool", "-overwrite_original", "-all=", "-Owner="+owner, path))
}

func run(cmd *exec.Cmd) error {
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", strings.Join(cmd.Args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = bufio.NewReader(in).WriteTo(out)
	if err != nil {
		return err
	}
	return out.Sync()
}

func normalizeExt(s string) string { return strings.TrimPrefix(strings.ToLower(s), ".") }
func sameExt(a, b string) bool     { return strings.EqualFold(filepath.Ext(a), filepath.Ext(b)) }
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
func human(n int64) string {
	units := []string{"B", "KB", "MB", "GB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}
