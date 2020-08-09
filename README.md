# Getting started

## Starting/destroying session
``` go
conn, err := filemaker.Connect("https://example.com", "database", "username", "password")
if err != nil {
  fmt.Println("Error:", err.Error())
  return
}
defer conn.Close()
```

## Perform find
``` go
var command = NewFindCommand(
  NewFindRequest(
    NewFindCriterion("fieldname", "=matchthis"),
  ),
  ...
)

records, err := conn.PerformFind("layoutname", command)
if err != nil {
  fmt.Println("Error:", err.Error())
}

for _, record := range records {
  fmt.Println(record["fieldname"])
}
```

### Omit
``` go
var command = NewFindCommand(
  NewFindRequest(
    NewFindCriterion("fieldname", "somethinglikethis"),
  ),
  NewFindRequest(
    NewFindCriterion("otherfieldname", "=notsomethinglikethis"),
  ).Omit(), //Omit request
)
```

### Limit
``` go
var command = NewFindCommand(
  ...
).SetLimit(10)
```

### Offset
``` go
var command = NewFindCommand(
  ...
).SetOffset(10)
```

### Limit and offset (chaining)
``` go
var command = NewFindCommand(
  ...
).SetLimit(10).SetOffset(10)
```
