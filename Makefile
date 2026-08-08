.PHONY: all frontend backend build clean

all: build

frontend:
	cd web && (npm ci || npm install) && npm run build

backend:
	go mod tidy
	# -trimpath: without it the builder's absolute paths (including their
	# username) end up inside the binary — needlessly disclosed, and it breaks
	# reproducible builds.
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o salt .

build: frontend backend

clean:
	rm -rf web/dist web/node_modules salt
