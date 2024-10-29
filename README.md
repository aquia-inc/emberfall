[![Go Report Card](https://goreportcard.com/badge/github.com/aquia-inc/emberfall)](https://goreportcard.com/report/github.com/aquia-inc/emberfall) 
# Emberfall

<img align="right" width="200" src="./emberfall-logo.png">
**HTTP smoke testing made easy.**

Simply declare a list of URLs and their expected response values, and Emberfall will test, compare, and report which URLs fail along with details of what expectations were not met.

## Configuring Tests

Tests are defined in a simple YAML document like the one below:
```yaml
commonHeaders: &commonHeaders
  authorization: "<authorization header>"
tests:
- url:
  method:
  expect:
- url: http://localhost:3000/api/v1/users/current
  method: GET
  expect:
    status: 401
    body: "unauthorized"

- url: http://localhost:3000/api/v1/users/current
  method: GET
  headers:
    <<: *commonHeaders
  expect:
    status: 200
    headers:
      content-type: "application/json"
```
Tests are merely a list of request objects, each with a url, method, headers to be sent, and an expects field. With the `expects` field you can define the status code, body contents (as a string), and any headers (as strings) that should be present in the response. If anything expected is not present or not equal to the defined value `emberfall` will exit with a non-zero code.

The YAML tests config can be provided in two ways:
- as a file: `emberfall --config path/to/config.yaml`
- piped to stdin: `echo $EMBERFALL_CONFIG | emberfall --config -` 

Piping to stdin is helpful when wanting to embed the config as a raw string in other yaml files such as Github Actions workflows (see below).


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

Coming soon!
