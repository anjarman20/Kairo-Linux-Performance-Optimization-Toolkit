VERSION  ?= 0.6.0
DEB_ARCH ?= $(shell go env GOARCH)
BIN     := bin/kairo
DEB     := dist/kairo_$(VERSION)_$(DEB_ARCH).deb
STAGE   := /tmp/kairo-deb/$(VERSION)
DEB_OUT := /tmp/kairo-deb/kairo_$(VERSION)_$(DEB_ARCH).deb

.PHONY: all build test vet fmt clean install deb build-deb

all: fmt vet test build

build:
	go build -o $(BIN) ./cmd/kairo

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

clean:
	rm -rf bin dist

install:
	go install ./cmd/kairo

deb: build-deb

# build-deb assembles a Debian/Ubuntu package. The layout is staged under
# /tmp (a real POSIX filesystem with operable permission bits) and the final
# .deb is copied into dist/, so the build also works on exotic mounts such as
# WSL drvfs that ignore chmod on directories.
build-deb:
	rm -rf dist $(STAGE) && mkdir -p dist
	mkdir -p $(STAGE)/DEBIAN $(STAGE)/usr/bin $(STAGE)/etc/kairo \
		$(STAGE)/var/lib/kairo $(STAGE)/var/log/kairo \
		$(STAGE)/usr/share/doc/kairo
	go build -trimpath -o $(STAGE)/usr/bin/kairo ./cmd/kairo
	sed -e 's/@VERSION@/$(VERSION)/' -e 's/@ARCH@/$(DEB_ARCH)/' \
		packaging/control.in > $(STAGE)/DEBIAN/control
	printf '\n' >> $(STAGE)/DEBIAN/control
	install -m0755 packaging/postinst $(STAGE)/DEBIAN/postinst
	install -m0644 packaging/copyright $(STAGE)/usr/share/doc/kairo/copyright
	install -m0644 README.md $(STAGE)/usr/share/doc/kairo/README.md
	chmod 0755 $(STAGE) $(STAGE)/DEBIAN
	dpkg-deb --build --root-owner-group $(STAGE) $(DEB_OUT)
	mkdir -p dist
	cp $(DEB_OUT) dist/
	@echo "built: $(DEB)"