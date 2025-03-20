[![Go Report Card](https://goreportcard.com/badge/github.com/aquia-inc/emberfall)](https://goreportcard.com/report/github.com/aquia-inc/emberfall) 
# Emberfall

<img align="right" width="200" src="./emberfall-logo.png">

**HTTP smoke testing made easy.**

Simply declare a list of URLs and their expected response values, and Emberfall will test, compare, and report which URLs fail along with details of what expectations were not met. Tests are merely a list of request objects, each with a url, method, headers to be sent, and an expects field. With the `expects` field you can define the status code, body contents (as a string), and any headers (as strings) that should be present in the response. If anything expected is not present or not equal to the defined value `emberfall` will exit with a non-zero code.


## Configuring Tests

The YAML tests config can be provided in two ways:
- as a file: `emberfall --config path/to/config.yaml`
- piped to stdin: `echo $EMBERFALL_CONFIG | emberfall --config -` 

Tests are defined in a simple YAML document with the following schema:
```yaml
tests:
- id: string # optional. used to cache the test for referencing in later tests. See Response References below
  url: string
  method: string # a supported HTTP method such as GET, POST, PUT, DELETE, etc...
  follow: bool # optional, whether to follow redirects or not, defaults to false
  headers: object # optional, sets headers to be sent with the request
    # arbitrary key:value pairs
  body: object # optional
    text: string # to send as content-type text/plain
    json: object # to send as content-type application/json
      # arbitrary key:value pairs
  expect:
    status: int #a supported HTTP status code such as 200,201,301,400,404, etc...
    body: object # optional
      text: string # to compare to the response body as a text string
      json: object # to compare to the response body as a json object
    headers: object #optional
      # key:value where header key is expected to be present in the response
```
> **_NOTE:_**  When expecting a JSON response, every key:value pair in the `expect.body.json` object must be present in the response body and of the same type. If the response body contains additional keys, the test will still pass. A future version will allow for a "strict" mode that will fail if the response body does not match the expected body exactly. An additional future release, will allow the use of a JSON schema to validate the response body which will be useful for validating reponses against types when actual values are not known or do not matter.

### Basic Tests Configuration
The following example is the minimum configuration required to execute Emberfall tests. This performs a single test against an API endpoint, expecting to receive a 401 unauthorized response:
```yaml
tests:
- url: http://localhost:3000/api/v1/users/current
  method: GET
  expect:
    status: 401
    body: "unauthorized"
```

### Reponse References
You can reference the response of a previous test in the current test. This is useful for testing endpoints that require a value from a previous response, for example testing that an API can create a resource and then retrieve it with the dynamically generated ID. Take a look at the following example, where the ID of the created user is referenced in the second test to retrieve the user by ID. The `{{.createUser.Response.id}}` syntax is used to reference the ID generated in the first test:
```yaml
tests:
- id: createUser
  url: http://localhost:3000/api/v1/users
  method: POST
  body:
    json:
      name: "John Doe"
  expect:
    status: 201
    body:
      json:
        name: "John Doe"

  # reference the user ID provided in the previous response
- url: "http://localhost:3000/api/v1/users/{{.createUser.Response.id}}"
  method: GET
  body:
    json:
      name: "John Doe"
  expect:
    status: 201
    body:
      json:
        name: "John Doe"

```
### Advanced Tests Configuration
The following highlights ways to leverage YAML anchors for common values between tests. Here we use `commonHeaders` (an arbitrary name) as a YAML anchor to reuse when needed:
```yaml
commonHeaders: &commonHeaders
  # example base64 encoded JWT for a local test user
  authorization: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJlbWFpbCI6InNvbWVvbmVAZXhhbXBsZS5jb20ifQ.54aXRXGjRGG7ft3aZ-Y75CVqq-falx5sgIhrafjzD-g"
tests:
  # ensure lack of authorization header returns 401
- url: http://localhost:3000/api/v1/users/current
  method: GET
  expect:
    status: 401
    body: "unauthorized"
  
  # ensure including authorization header returns current user
- url: http://localhost:3000/api/v1/users/current
  method: GET
  headers:
    <<: *commonHeaders # reference YAML anchor to reuse values
  expect:
    status: 200
    headers:
      content-type: "application/json"
```
## Testing locally

### Installation

**Homebrew**
```sh
brew tap aquia-inc/emberfall https://github.com/aquia-inc/emberfall
brew install emberfall
```
**Build from Source**
1. Fork or clone this repository
2. From the project root run
```bash
go install ./...
```

### Running Tests

Define tests in a YAML file like show above, and run emberfall: `emberfall --config path/to/config.yaml`

## As a Github Action


```yaml
  uses: "aquia-inc/emberfall@main"
  with:
   version: 0.3.2
   config: # string: YAML tests config inlined
   file: # string: path/to/tests
```

> **_NOTE:_** when both `config` and `file` are specified, Emberfall will run _twice_ with `config` running first, and `file` running second.

### Inlined Tests

This is helpful for either short tests or for testing Emberfall integration with your workflow

```yaml
  uses: "aquia-inc/emberfall@main"
  with:
   version: 0.3.2
   config: | 
    ---
    tests:  
      - url: https://github.com
        method: GET
        expect:
          status: 200
          headers:
            server: GitHub.com
            content-type: "text/html; charset=utf-8"
            content-language: en-US
      
```

### Tests in a file
For longer tests it's best to place those in their own file like so
```yaml
  uses: "aquia-inc/emberfall@main"
  with:
   version: 0.3.2
   file: path/to/tests.yml   
```
