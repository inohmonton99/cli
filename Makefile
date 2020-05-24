init:
ifndef version
	err = $(error version is undefined)
	$(err)
endif

	# Update version
	echo "package config\n\nconst VersionTag = \"v${version}\"" > config/version.go

build-linux-amd64: init
	env GOOS=linux GOARCH=amd64 go build -o opctl-linux-amd64 main.go

build-macos-amd64: init
	env GOOS=darwin GOARCH=amd64 go build -o opctl-macos-amd64 main.go

build-windows-amd64: init
	env GOOS=windows GOARCH=amd64 go build -o opctl-windows-amd64.exe main.go

all: build-linux-amd64 build-macos-amd64 build-windows-amd64