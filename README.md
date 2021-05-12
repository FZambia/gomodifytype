Modify Go struct field type. It matches type by its string representation and replaces to another string.

Mostly useful for primitive types or types located in the same package. Maybe can be useful for external package types if followed by `goimports`.

My use case was replacing `[]byte` type in a Protobuf generated code to custom `Raw` type.

Like this:

```
gomodifytype -file proxy.pb.go -all -w -from "[]byte" -to "Raw"
```

Thanks to https://github.com/fatih/gomodifytags for the AST modification example.
