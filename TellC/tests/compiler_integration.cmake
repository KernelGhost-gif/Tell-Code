if(NOT DEFINED TELL_COMPILER OR NOT DEFINED TELL_SOURCE_DIR OR NOT DEFINED TELL_WORK_DIR)
  message(FATAL_ERROR "TELL_COMPILER, TELL_SOURCE_DIR, and TELL_WORK_DIR are required")
endif()

file(MAKE_DIRECTORY "${TELL_WORK_DIR}")

function(compile_and_run name source expected_stdout)
  set(executable "${TELL_WORK_DIR}/${name}")
  execute_process(
    COMMAND "${TELL_COMPILER}" "${TELL_SOURCE_DIR}/${source}" -o "${executable}"
    RESULT_VARIABLE compile_result
    OUTPUT_VARIABLE compile_stdout
    ERROR_VARIABLE compile_stderr
  )
  if(NOT compile_result EQUAL 0)
    message(FATAL_ERROR
      "Expected ${source} to compile, but tellc exited ${compile_result}.\nstdout:\n${compile_stdout}\nstderr:\n${compile_stderr}")
  endif()

  execute_process(
    COMMAND "${executable}"
    RESULT_VARIABLE run_result
    OUTPUT_VARIABLE actual_stdout
    ERROR_VARIABLE run_stderr
  )
  if(NOT run_result EQUAL 0)
    message(FATAL_ERROR
      "Expected ${source} executable to exit 0, got ${run_result}.\nstderr:\n${run_stderr}")
  endif()
  if(NOT "${actual_stdout}" STREQUAL "${expected_stdout}")
    message(FATAL_ERROR
      "Unexpected output from ${source}.\nExpected: [${expected_stdout}]\nActual:   [${actual_stdout}]\nstderr:\n${run_stderr}")
  endif()
  message(STATUS "PASS: ${source}")
endfunction()

function(expect_compile_failure name source expected_diagnostic)
  set(executable "${TELL_WORK_DIR}/${name}")
  execute_process(
    COMMAND "${TELL_COMPILER}" "${TELL_SOURCE_DIR}/${source}" -o "${executable}"
    RESULT_VARIABLE compile_result
    OUTPUT_VARIABLE compile_stdout
    ERROR_VARIABLE compile_stderr
  )
  if(compile_result EQUAL 0)
    message(FATAL_ERROR "Expected ${source} to fail compilation, but it succeeded")
  endif()
  string(FIND "${compile_stderr}" "${expected_diagnostic}" diagnostic_position)
  if(diagnostic_position EQUAL -1)
    message(FATAL_ERROR
      "Compilation of ${source} failed for the wrong reason. Expected diagnostic [${expected_diagnostic}].\nstdout:\n${compile_stdout}\nstderr:\n${compile_stderr}")
  endif()
  message(STATUS "PASS: ${source} rejected with expected diagnostic")
endfunction()

compile_and_run(factorial examples/factorial.tell "120\n")
compile_and_run(objects examples/objects.tell "true\nfalse\n")
expect_compile_failure(private_field tests/fixtures/private_field.tell "field 'secret' is private")
expect_compile_failure(argument_type tests/fixtures/argument_type.tell "expected 'number', got 'text'")
expect_compile_failure(return_type tests/fixtures/return_type.tell "expected 'number', got 'text'")
expect_compile_failure(missing_return tests/fixtures/missing_return.tell "function must return a value on every path")
