setup() {
    export BATS_LIB_PATH=${BATS_LIB_PATH:-"/usr/lib"}
    bats_load_library bats-support
    bats_load_library bats-assert
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
  run ./emberfall --config ./tests/pass-missing-headers.yml
  assert_failure
  assert_output --partial 'FAIL'
  assert_output --partial 'expected header x-no-exist was missing'
}

