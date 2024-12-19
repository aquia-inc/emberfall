setup() {
    export BATS_LIB_PATH=${BATS_LIB_PATH:-"/usr/lib"}
    bats_load_library bats-support
    bats_load_library bats-assert
}

@test "--version should be correct" {
  run ./emberfall --version
  assert_success
  assert_output "emberfall version 0.2.0"
}

@test "no config SHOULD FAIL" {
    run ./emberfall
    assert_failure
    assert_output --partial 'no config provided'
}

@test "SHOULD FAIL without following redirect" {
  run ./emberfall --config ./tests/fail-no-follow.yml
  assert_failure
  assert_output --partial 'FAIL'
  assert_output --partial 'expected status == 200 got 301'
}

@test "SHOULD PASS by following redirect" {
  run ./emberfall --config ./tests/pass-follow.yml
  assert_success
  assert_output --partial 'PASS'
}

@test "SHOULD PASS with expected headers" {
  run ./emberfall --config ./tests/pass-headers.yml
  assert_success
  assert_output --partial 'PASS'
}

@test "SHOULD FAIL with missing headers" {
  run ./emberfall --config ./tests/fail-missing-headers.yml
  assert_failure
  assert_output --partial 'FAIL'
  assert_output --partial 'expected header x-no-exist was missing'
}

@test "SHOULD FAIL with bad url" {
  run ./emberfall --config ./tests/fail-bad-url.yml
  assert_failure
  assert_output --partial 'no such host'
}

@test "SHOULD PASS with response JSON == request JSON" {
  run ./emberfall --config ./tests/pass-req-res-json-match.yml
  assert_success
}

@test "SHOULD FAIL with response JSON != request JSON" {
  run ./emberfall --config ./tests/fail-req-res-json-no-match.yml
  assert_failure
  assert_output --partial 'expected body.json.data.foo == baz got bar'
}
