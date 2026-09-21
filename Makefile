.PHONY: build install clean

build:
	go build -trimpath -ldflags='-s -w' -o tinyimg .

install:
	go install -trimpath -ldflags='-s -w' .

clean:
	rm -f tinyimg
