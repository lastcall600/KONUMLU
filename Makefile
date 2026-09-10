.PHONY: arch-check
arch-check:
	go -C backend run ./cmd/archcheck
