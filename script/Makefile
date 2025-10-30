LOCALES := zh_CN en_US

run:
	@for locale in $(LOCALES); do \
		go run ./cmd/scriptgen/main.go locale.json $$locale; \
	done
