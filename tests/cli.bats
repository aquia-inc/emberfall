@test "emberfall with no config" {
    run ./emberfall
    assert_failure
}
