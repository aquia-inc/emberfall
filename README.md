[![Go Report Card](https://goreportcard.com/badge/github.com/aquia-inc/emberfall)](https://goreportcard.com/report/github.com/aquia-inc/emberfall) 
# Emberfall

<img align="right" width="200" src="./emberfall-logo.png">

**HTTP smoke testing made easy.**

Simply declare a list of URLs and their expected response values, and Emberfall will test, compare, and report which URLs fail along with details of what expectations were not met.

## Configuring Tests
Tests are merely a list of request objects, each with a url, method, headers to be sent, and an expects field. With the `expects` field you can define the status code, body contents (as a string), and any headers (as strings) that should be present in the response. If anything expected is not present or not equal to the defined value `emberfall` will exit with a non-zero code.

The YAML tests config can be provided in two ways:
- as a file: `emberfall --config path/to/config.yaml`
- piped to stdin: `echo $EMBERFALL_CONFIG | emberfall --config -` 

Tests are defined in a simple YAML document that defines the following keys:
```yaml
tests:
- url: string
  method: string #a supported HTTP method such as GET, POST, PUT, DELETE, etc...
  follow: bool # optional, whether to follow redirects or not, defaults to false
  expect:
    status: int #a supported HTTP status code such as 200,201,301,400,404, etc...
    body: string # optional
    headers: #optional
      # key:value where header key is expected to be present in the response
```

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
   version: 0.1.0
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
