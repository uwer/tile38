## Tile38 Integration Testing

- Uses Redis protocol
- The Tile38 data is flushed before every `DoBatch`

A basic test operation looks something like:

```go
func keys_SET_test(mc *mockServer) error {
	return mc.DoBatch([][]interface{}{
        {"SET", "fleet", "truck1", "POINT", 33.0001, -112.0001}, {"OK"},
        {"GET", "fleet", "truck1", "POINT"}, {"[33.0001 -112.0001]"},
    }
}
```

Using a custom function:

```go
func keys_MATCH_test(mc *mockServer) error {
	return mc.DoBatch([][]interface{}{
        {"SET", "fleet", "truck1", "POINT", 33.0001, -112.0001}, {
            func(v interface{}) (resp, expect interface{}) {
                // v is the value as strings or slices of strings
                // test will pass as long as `resp` and `expect` are the same.
                return v, "OK"
            },
		},
    }
}
```


