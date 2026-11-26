BINARY := coolhome

.PHONY: run demo build test vet fmt check clean miniapp-test web-check web-test check-all

run:
	go run ./cmd/coolhome

demo:
	go run ./cmd/coolhome -demo

build:
	go build -o bin/$(BINARY) ./cmd/coolhome

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

check: fmt vet test

# Tests for the Tuya mini-app bundle: pure helpers, the API client and the page
# handlers, all exercised against a real CoolHome server in demo mode.
miniapp-test:
	node --check miniapp/utils/api.js
	node --check miniapp/utils/format.js
	node --check miniapp/utils/view.js
	node --check miniapp/pages/home/index.js
	node --check miniapp/pages/device/index.js
	node --test miniapp-tests/

# The dashboard is plain JavaScript with no build step, so a syntax check is
# the only gate it gets.
web-check:
	node --check web/static/app.js
	node --check web/static/battery.js
	node --check web/static/figures.js
	node --check web/static/panel.js
	node --check web/static/names.js
	node --check web/static/rules.js
	node --check web/static/power.js

# Rules the dashboard applies to device data, tested without a browser.
web-test:
	node --test web-tests/

check-all: check web-check web-test miniapp-test

clean:
	rm -rf bin
