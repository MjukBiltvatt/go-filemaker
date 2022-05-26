go-filemaker is a simple Go (Golang) wrapper for the [FileMaker Data API](https://fmhelp.filemaker.com/docs/18/en/dataapi), inspired by the FileMaker PHP API. It's been tested and verified to work with FileMaker Server 18 and 19.

# Getting started

## Installation
```
go mod init github.com/my/repo
go get github.com/MjukBiltvatt/go-filemaker/v3
```

## Importing
``` go
import "github.com/MjukBiltvatt/go-filemaker/v2"
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
record := fm.CreateRecord("layoutname")
record.SetField("fieldname", "data")

err := record.Commit()
if err != nil {
  //... handle error
}

fmt.Println("Record ID:", record.ID) //Record now contains an ID
```

### Edit
``` go
record.SetField("fieldname", "new data")

err := record.Commit()
if err != nil {
  //... handle error
}
```

### Revert uncommitted changes
``` go
record.SetField("fieldname", "new data")
record.Revert()
```

### Delete
``` go
err := record.Delete()
if err != nil {
  //... handle error
}
```

### Commit file or byte buffer to container field
Container fields are a little bit different from normal fields, so we can't simply set the container field data and commit it with the rest of the record data. We instead need to commit the container field data seperately. The record must already be committed or be the result of a find command. An error will be returned if attempting this before committing a newly created record.
``` go
//Commit a byte buffer
err := record.CommitToContainer("fieldname", "filename.pdf", buf)

//Commit a file, the contents will be copied
err := record.CommitFileToContainer("fieldname", "/path/to/my/file.pdf")
```

### Get field data

#### String
The FileMaker database field needs to be of type text.
``` go
val, err := record.String("fieldname")
```

#### Int
The FileMaker database field needs to be of type number.
``` go
val, err := record.Int("fieldname")
```

#### Int32
The FileMaker database field needs to be of type number.
``` go
val, err := record.Int32("fieldname")
```

#### Int64
The FileMaker database field needs to be of type number.
``` go
val, err := record.Int64("fieldname")
```

#### Float32
The FileMaker database field needs to be of type number.
``` go
val, err := record.Float32("fieldname")
```

#### Float64
The FileMaker database field needs to be of type number.
``` go
val, err := record.Float64("fieldname")
```

#### Bool
The FileMaker database field needs to be of type number. Values greater than `0` will return `true`, otherwise `false`.
``` go
val, err := record.Bool("fieldname")
```

#### Interface
If for some reason you want an `interface{}` use the `GetField()` method. Good to know here is that FileMaker number fields will always be of type `float64`.
``` go
val := record.GetField("fieldname")
```
### Map field data to struct
``` go
type Person struct {
  Firstname string `fm:"firstname"`
  Lastname  string `fm:"lastname"`
  Age       int    `fm:"age"`
}

myPerson := Person{}
fmt.Println(myPerson) //Output: {  0}

record.Map(&myPerson)
fmt.Println(myPerson) //Output: {Test Testsson 23}
```
