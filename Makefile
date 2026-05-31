.PHONY: editor-ui

# Build the browser graph editor frontend and embed it into pkg/grapheditor/static.
editor-ui:
	cd web && npm ci && npm run build
