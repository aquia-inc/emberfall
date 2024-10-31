setup() {
    export BATS_LIB_PATH=${BATS_LIB_PATH:-"/usr/lib"}
    bats_load_library bats-support
    bats_load_library bats-assert
}

@test "emberfall with no config SHOULD FAIL" {
    run ./emberfall
    assert_failure
}
