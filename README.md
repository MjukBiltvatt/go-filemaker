go-filemaker is a simple Go (Golang) wrapper for the [FileMaker Data API](https://fmhelp.filemaker.com/docs/18/en/dataapi), inspired by the FileMaker PHP API. It's been tested and verified to work with FileMaker Server 18 and 19.

# Getting started

## Installation

```
go mod init github.com/my/repo
go get github.com/MjukBiltvatt/go-filemaker/v3
```

## Importing

``` go
import "github.com/MjukBiltvatt/go-filemaker/v3"
```

## Quickstart
***By default the returned records limit is 100.*** This can however be controlled with [Limit](#limit).

``` go
//Create a session
fm, err := filemaker.New(
  "https://my.host.com",
  "database",
  "username",
  "password",
)
if err != nil {
  fmt.Printf("Failed to start session: %s", err.Error())
  return
}
//Destroy the session when we're done with it
defer fm.Destroy()

//Perform find command
records, err := fm.Find(
  "layoutname",
  filemaker.NewFindCommand(
    filemaker.NewFindRequest(
      filemaker.NewFindCriterion("Firstname", "Mark"),
      filemaker.NewFindCriterion("HasCat", ".."),
      filemaker.NewFindCriterion("Age", "*"),
    ),
    filemaker.NewFindRequest(
      filemaker.NewFindCriterion("Lastname", "==Johnson"),
    ).Omit()
  ).Limit(10)
)
if err != nil {
  fmt.Printf("Failed to perform find: %s", err.Error())
  return
}

//Evaluate result
if len(records) > 0 {
  for _, record := range records {
    fmt.Printf(
      "%s is %d years old.",
      record.String("Firstname"),
      record.Int("Age"),
    )
  }
} else {
  fmt.Println("No records found")
}
```

### FindCommand
While being able to pass findrequests into the `NewFindCommand` method, they can also be added to the findcommand after instantiation.

***By default the limit is 100.***

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
Omit any records that match the find request.

``` go
command := filemaker.NewFindCommand(
  filemaker.NewFindRequest(
    filemaker.NewFindCriterion("field name", "somethinglikethis"),
  ),
  filemaker.NewFindRequest(
    filemaker.NewFindCriterion("other field name", "notsomethinglikethis"),
  ).Omit(), //Omit request
)
```

### Limit
Will limit the number of records returned from a find command.

***By default the limit is 100.***

``` go
command := filemaker.NewFindCommand(
  //...
).Limit(10)
```

### Offset
Will offset the records returned from a find command.

``` go
command := filemaker.NewFindCommand(
  //...
).Offset(10)
```

### Limit and offset (chaining)
Both of these can be chained, allowing them to be used directly in the `Find` method.

``` go
records, err := fm.Find(
  "layout name",
  filemaker.NewFindCommand(
    //...
  ).Limit(10).Offset(10)
)
```

## Records

### Create

``` go
//Create a new empty record for the specified layout
record := fm.NewRecord("layout name")

//Set field data
record.Set("field name", "data")

//Create the record
err := record.Commit()

//Record now contains an ID after committing
fmt.Printf("Record ID: %v", record.ID)
```

### Edit

``` go
//Set field data
record.Set("field name", "new data")

//Commit changes
err := record.Commit()
```

### Revert uncommitted changes

``` go
//Set field data
record.Set("field name", "new data")

//Reset the record
record.Reset()

fmt.Println(record.String("field name"))
//Output: original data
```

### Delete

``` go
err := record.Delete()
```

### Commit file or byte buffer to container field
Container fields are a little bit different from normal fields, so we can't simply set the container field data and commit it with the rest of the record data. We instead need to commit the container field data seperately. The record must already be committed or be the result of a find command. An error will be returned if attempting this before committing a newly created record.

``` go
//Commit a byte buffer
err := record.CommitToContainer("field name", "filename.pdf", buf)

//Commit a file, the contents will be copied
err := record.CommitFileToContainer("field name", "/path/to/my/file.pdf")
```

### Get field data

#### String
*The FileMaker database field needs to be of type text.*

``` go
val := record.String("field name")

//With error (ErrNotString if not a string)
val, err := record.StringE("field name")
```

#### Int
*The FileMaker database field needs to be of type number.*

``` go
val := record.Int("field name")

//With error (ErrNotNumber if not a number)
val, err := record.IntE("field name")
```

#### Int8
*The FileMaker database field needs to be of type number.*

``` go
val := record.Int8("field name")

//With error (ErrNotNumber if not a number)
val, err := record.Int8E("field name")
```

#### Int16
*The FileMaker database field needs to be of type number.*

``` go
val := record.Int16("field name")

//With error (ErrNotNumber if not a number)
val, err := record.Int16E("field name")
```

#### Int32
*The FileMaker database field needs to be of type number.*

``` go
val := record.Int32("field name")

//With error (ErrNotNumber if not a number)
val, err := record.Int32E("field name")
```

#### Int64
*The FileMaker database field needs to be of type number.*

``` go
val := record.Int64("field name")

//With error (ErrNotNumber if not a number)
val, err := record.Int64E("field name")
```

#### Float32
*The FileMaker database field needs to be of type number.*

``` go
val := record.Float32("field name")

//With error (ErrNotNumber if not a number)
val, err := record.Float32E("field name")
```

#### Float64
*The FileMaker database field needs to be of type number.*

``` go
val := record.Float64("field name")

//With error (ErrNotNumber if not a number)
val, err := record.Float64E("field name")
```

#### Bool
Will return `false` for empty fields and number fields that evaluate to `0` or less - will return `true` otherwise.

``` go
val := record.Bool("field name")
```

#### Time

Will attempt to parse field value into a `time.Time` object. Supported formats:

- `01/02/2006`
- `01/02/2006 15:04:05`
- `2006-01-02`
- `2006-01-02 15:04:05`

``` go
val := record.Time("field name")

//With error (ErrUnknownFormat if not a valid format. May also return time.Parse errors.)
val, err := record.TimeE("field name")
```

#### Interface
If for some reason you want an `interface{}`, use the `Get()` method. Keep in mind though that FileMaker number fields will be of type `float64` - text, date and timestamp fields will be of type `string`.

``` go
val := record.Get("field name")
```

### Map field data to struct

Infinitely nested structs are supported.

``` go
type Hero struct {
  TimestampCreated  time.Time   `fm:"TimestampCreated"`
  Firstname         string      `fm:"Firstname"`
  Lastname          string      `fm:"Lastname"`
  Age               int         `fm:"Age"`
  BadassRating      float64     `fm:"BadassRatingOf100"`
  DateFirstPurchase time.Time   `fm:"DateFirstProductPurchase"`
  IsBadass          bool        `fm:"Badass"`
}

var hero Hero

//Print before
//Output: {TimestampCreated:0001-01-01 00:00:00 +0000 UTC Firstname: Lastname: Age:0 BadassRating:0 DateFirstPurchase:0001-01-01 00:00:00 +0000 UTC IsBadass:false}
fmt.Printf("%+v\n", hero)

//Map record fields to struct fields
record.Map(&hero)

//Print after
//Output: {TimestampCreated:2006-01-02 15:04:05 +0100 CET Firstname:Volodymyr Lastname:Zelenskyj Age:44 BadassRating:99.99 DateFirstPurchase:2006-01-02 00:00:00 +0100 CET IsBadass:true}
fmt.Printf("%+v\n", hero)
```
