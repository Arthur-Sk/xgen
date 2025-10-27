go-update:
  go get -u -t ./... && just go-tidy

go-tidy:
  go mod tidy

go-build:
  go build ./cmd/xgen/xgen.go

xsd-gen:
  ./xgen -p out -i source/common_types.xsd -o out/commonTypes.go -l Go
