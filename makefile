NAME=go-template
DISTDIR=dist
VERSION=$(shell git describe --tags || echo "dev")
GOBUILD=go build -ldflags "-s -w -X 'go-template/common/config.Version=$(VERSION)'"

all: build

build:
	@mkdir -p $(DISTDIR)
	$(GOBUILD) -o $(DISTDIR)/$(NAME)

clean:
	rm -rf $(DISTDIR)
