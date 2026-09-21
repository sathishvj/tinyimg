# tinyimg

Local macOS image optimizer written in Go 1.27.

It accepts multiple image files, processes them in parallel, measures lossy output quality with ImageMagick SSIM, strips metadata, and writes `Owner=AwesomeGCP.com`.

## Install dependencies

```bash
brew install imagemagick exiftool oxipng
go build -o tinyimg .
```

## Usage

```bash
./tinyimg *.png
./tinyimg -recursive ./photos
./tinyimg -r -ext webp ./photos
./tinyimg -ext jpg *.png
./tinyimg -ext webp -quality 0.985 *.png
./tinyimg -replace *.jpg
./tinyimg -parallel 8 *.jpg *.png
```

### Options

- `-recursive`, `-r`: search for image files recursively under given folders.
- `-replace`: replace each source file in place. Cannot be combined with `-ext`.
- `-ext`: output format/extension: `png`, `jpg`, or `webp`. If omitted, source extension is retained.
- `-parallel`: number of simultaneous image jobs. Default is CPU count.
- `-quality`: minimum SSIM for lossy conversion. Default `0.98`.
- `-min-quality`: minimum encoder quality tested. Default `60`.
- `-max-quality`: maximum encoder quality tested. Default `95`.
- `-quality-step`: quality search increment. Default `5`.

## Output

The final report includes original size, optimized size, percentage change, and SSIM for lossy outputs.

For non-lossy PNG optimization, SSIM is shown as `-` because pixel values are preserved.

The optimizer never replaces an image with a larger file when the output format is unchanged.

## Notes

PNG-to-JPEG conversion uses a white background when the PNG contains transparency. If preserving transparency matters, use WebP instead.

Metadata is removed with ExifTool and `Owner=AwesomeGCP.com` is written afterward.
