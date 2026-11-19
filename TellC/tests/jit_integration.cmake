file(MAKE_DIRECTORY "${TELL_WORK_DIR}")

function(check_jit source expected_status expected_stdout expected_stderr)
  execute_process(
    COMMAND "${TELL_COMPILER}" --jit "${TELL_SOURCE_DIR}/${source}"
    WORKING_DIRECTORY "${TELL_WORK_DIR}"
    RESULT_VARIABLE status
    OUTPUT_VARIABLE stdout
    ERROR_VARIABLE stderr
  )
  if(NOT "${status}" STREQUAL "${expected_status}" OR
     NOT "${stdout}" STREQUAL "${expected_stdout}" OR
     NOT "${stderr}" MATCHES "${expected_stderr}")
    message(FATAL_ERROR "Unexpected JIT result for ${source} (${status}).\nstdout:\n${stdout}\nstderr:\n${stderr}")
  endif()
endfunction()

check_jit(examples/factorial.tell 0 "120\n" "^$")
check_jit(examples/objects.tell 0 "true\nfalse\n" "^$")
check_jit(tests/fixtures/uncaught_error.tell 1 "" "^Error: invalid number\n$")
check_jit(tests/fixtures/argument_type.tell 1 "" "argument_type.tell:[0-9]+:[0-9]+:.*expected 'number', got 'text'")

if(EXISTS "${TELL_WORK_DIR}/a.out")
  message(FATAL_ERROR "JIT execution wrote an executable")
endif()
