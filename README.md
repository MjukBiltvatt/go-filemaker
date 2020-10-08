go-filemaker is a simple Go wrapper for the [FileMaker Data API](https://fmhelp.filemaker.com/docs/18/en/dataapi), heavily inspired by the FileMaker PHP API.

# [Version 2 now available](v2/README.md)

# Getting started

## Installation
```
go mod init github.com/my/repo
go get github.com/jomla97/go-filemaker
```

## Importing
``` go
import "github.com/jomla97/go-filemaker"
```

## Starting/destroying session
``` go
fm, err := filemaker.New("https://example.com", "database", "username", "password")
if err != nil {
  fmt.Println("Error:", err.Error())
  return
}
defer fm.Destroy()
```
## Resuming a session
A session can be resumed as long as it hasn't been destroyed. All that's needed in addition to all parameters in `New()` is the session token string.
``` go
fm, err := filemaker.New("https://example.com", "database", "username", "password")
if err != nil {
  fmt.Println("Error:", err.Error())
  return
}
token := fm.Token

fmResumed, err := filemaker.Resume("https://example.com", "database", "username", "password", token)
if err != nil {
  fmt.Println("Error:", err.Error())
  return
}
```

## Perform find
``` go
command := filemaker.NewFindCommand(
  filemaker.NewFindRequest(
    filemaker.NewFindCriterion("fieldname", "=matchthis"),
    //... more criterions go here
  ),
  //... more requests go here
)

records, err := fm.PerformFind("layoutname", command)
if err != nil {
  fmt.Println("Error:", err.Error())
  return
}

if len(records) > 0 {
  for _, record := range records {
    fmt.Println(record.GetField("fieldname").(string))
  }
} else {
  fmt.Println("No records found")
}
```

### FindCommand
While being able to pass findrequests into the `NewFindCommand` method, they can also be added to the findcommand after instantiation.
``` go
command := filemaker.NewFindCommand()
command.AddRequest(request)
```

### FindRequest
While being able to pass findcriterions into the `NewFindRequest` method, they can also be added to the findrequest after instantiation.
``` go
request := filemaker.NewFindRequest()
request.AddCriterion(criterion)
```

### Omit
``` go
command := filemaker.NewFindCommand(
  filemaker.NewFindRequest(
    filemaker.NewFindCriterion("fieldname", "somethinglikethis"),
  ),
  filemaker.NewFindRequest(
    filemaker.NewFindCriterion("otherfieldname", "=notsomethinglikethis"),
  ).Omit(), //Omit request
)
```

### Limit
``` go
command := filemaker.NewFindCommand(
  //...
).SetLimit(10)
```

### Offset
``` go
command := filemaker.NewFindCommand(
  //...
).SetOffset(10)
```

### Limit and offset (chaining)
``` go
command := filemaker.NewFindCommand(
  //...
).SetLimit(10).SetOffset(10)
```

## Records

### Create
``` go
record := filemaker.CreateRecord("layoutname")
record.SetField("fieldname", "data")

err := fm.Commit(&record) //Need to pass record by pointer
if err != nil {
  //... handle error
  return
}

fmt.Println("Record ID:", record.ID) //Record now contains an ID
```

### Edit
``` go
record.SetField("fieldname", "new data")

err := fm.Commit(&record) //Need to pass record by pointer
```

### Delete
``` go
err := fm.Delete(record.Layout, record.ID)
```
