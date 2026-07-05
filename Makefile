all: install-requirements

.installed: requirements.txt
	pip install -r requirements.txt
	touch .installed

install-requirements: .installed

install: install-requirements
	install goveewatch /usr/bin/
