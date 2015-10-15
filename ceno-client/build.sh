#! /usr/bin/env sh

SOURCE_FILES="src/client.go \
    src/errorhandling.go \
    src/clientconfig.go \
    src/portal.go \
    src/articles.go"

echo "Formatting go files."
for file in `ls src/*.go`; do
  go fmt $file;
done

echo "Installing dependencies."
go get -u github.com/nicksnyder/go-i18n/goi18n

echo "Merging localization files."
cd translations
$GOPATH/bin/goi18n *.all.json *.untranslated.json
cd ..

go build $SOURCE_FILES && echo "Compiled client successfully."
