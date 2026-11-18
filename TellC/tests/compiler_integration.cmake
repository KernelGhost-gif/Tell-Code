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
compile_and_run(switch tests/fixtures/switch.tell "evaluated\nHalf the tank is full\nafter\nmatched text\nnested\ndone\n1\n2\ntwo\ndecimal\nboolean\n")
expect_compile_failure(switch_type tests/fixtures/switch_type.tell "expected 'number', got 'text'")
expect_compile_failure(switch_empty tests/fixtures/switch_empty.tell "expected an indented switch case")
compile_and_run(collections tests/fixtures/collections.tell "one\nevaluated\n1\n3\none\n4\n4\n")
expect_compile_failure(collection_type tests/fixtures/collection_type.tell "expected 'number', got 'text'")
expect_compile_failure(collection_iterate_type tests/fixtures/collection_iterate_type.tell "for each requires a list or vector")
expect_compile_failure(collection_empty_infer tests/fixtures/collection_empty_infer.tell "empty list requires an explicit collection type")
execute_process(COMMAND "${TELL_COMPILER}" "${TELL_SOURCE_DIR}/tests/fixtures/collection_bounds.tell" -o "${TELL_WORK_DIR}/collection_bounds" RESULT_VARIABLE bounds_compile ERROR_VARIABLE bounds_error)
if(NOT bounds_compile EQUAL 0)
  message(FATAL_ERROR "Bounds fixture did not compile: ${bounds_error}")
endif()
execute_process(COMMAND "${TELL_WORK_DIR}/collection_bounds" RESULT_VARIABLE bounds_result OUTPUT_QUIET ERROR_QUIET)
if(bounds_result STREQUAL "0")
  message(FATAL_ERROR "Out-of-bounds indexing should terminate the program")
endif()
compile_and_run(loops_assignment tests/fixtures/loops_assignment.tell "1\n3\n3\n1\n9\n8\n")
expect_compile_failure(assign_constant tests/fixtures/assign_constant.tell "cannot assign to constant 'value'")
expect_compile_failure(assign_type tests/fixtures/assign_type.tell "expected 'number', got 'text'")
expect_compile_failure(assign_index_type tests/fixtures/assign_index_type.tell "expected 'number', got 'text'")
execute_process(COMMAND "${TELL_COMPILER}" "${TELL_SOURCE_DIR}/tests/fixtures/io_errors.tell" -o "${TELL_WORK_DIR}/io_errors" RESULT_VARIABLE io_compile ERROR_VARIABLE io_compile_error)
if(NOT io_compile EQUAL 0)
  message(FATAL_ERROR "I/O fixture failed to compile: ${io_compile_error}")
endif()
file(WRITE "${TELL_WORK_DIR}/io_input.txt" "42\r\n")
execute_process(COMMAND "${TELL_WORK_DIR}/io_errors" WORKING_DIRECTORY "${TELL_WORK_DIR}" INPUT_FILE "${TELL_WORK_DIR}/io_input.txt" RESULT_VARIABLE io_result OUTPUT_VARIABLE io_output ERROR_VARIABLE io_error)
if(NOT io_result EQUAL 0 OR NOT io_output STREQUAL "Value? 42\nhello world\ncustom error\ncannot open file\nindex out of bounds\n2.5\n")
  message(FATAL_ERROR "I/O valid input failed: ${io_result}, [${io_output}], [${io_error}]")
endif()
file(WRITE "${TELL_WORK_DIR}/io_input.txt" "9223372036854775808\n")
execute_process(COMMAND "${TELL_WORK_DIR}/io_errors" WORKING_DIRECTORY "${TELL_WORK_DIR}" INPUT_FILE "${TELL_WORK_DIR}/io_input.txt" RESULT_VARIABLE io_result OUTPUT_VARIABLE io_output ERROR_VARIABLE io_error)
if(NOT io_result EQUAL 0 OR NOT io_output STREQUAL "Value? invalid number\nhello world\ncustom error\ncannot open file\nindex out of bounds\n2.5\n")
  message(FATAL_ERROR "I/O invalid input failed: ${io_result}, [${io_output}], [${io_error}]")
endif()
file(WRITE "${TELL_WORK_DIR}/io_input.txt" "")
execute_process(COMMAND "${TELL_WORK_DIR}/io_errors" WORKING_DIRECTORY "${TELL_WORK_DIR}" INPUT_FILE "${TELL_WORK_DIR}/io_input.txt" RESULT_VARIABLE io_result OUTPUT_VARIABLE io_output ERROR_VARIABLE io_error)
if(NOT io_result EQUAL 0 OR NOT io_output STREQUAL "Value? cannot read input\nhello world\ncustom error\ncannot open file\nindex out of bounds\n2.5\n")
  message(FATAL_ERROR "I/O EOF input failed: ${io_result}, [${io_output}], [${io_error}]")
endif()
execute_process(COMMAND "${TELL_COMPILER}" "${TELL_SOURCE_DIR}/tests/fixtures/uncaught_error.tell" -o "${TELL_WORK_DIR}/uncaught_error" RESULT_VARIABLE uncaught_compile ERROR_VARIABLE uncaught_compile_error)
if(NOT uncaught_compile EQUAL 0)
  message(FATAL_ERROR "Uncaught error fixture failed to compile: ${uncaught_compile_error}")
endif()
execute_process(COMMAND "${TELL_WORK_DIR}/uncaught_error" RESULT_VARIABLE uncaught_result OUTPUT_VARIABLE uncaught_output ERROR_VARIABLE uncaught_error)
if(NOT uncaught_result EQUAL 1 OR NOT uncaught_output STREQUAL "" OR NOT uncaught_error STREQUAL "Error: invalid number\n")
  message(FATAL_ERROR "Unexpected uncaught error: ${uncaught_result}, [${uncaught_output}], [${uncaught_error}]")
endif()
compile_and_run(mutable_collections tests/fixtures/mutable_collections.tell "5\n7\n9\n5\n2\n7\n2\n3\n4\n9\n8\ntwo\nitem not found in list\n2\n1\n3\n2\nfalse\n")
expect_compile_failure(mutable_type tests/fixtures/mutable_type.tell "expected 'number', got 'text'")
expect_compile_failure(mutable_target tests/fixtures/mutable_target.tell "add and remove require a list or vector")
compile_and_run(operators tests/fixtures/operators.tell "2\n7\n5\n-6\n16\n-2\n13\ntrue\nfalse\ntrue\ntrue\nfalse\ntrue\ninvalid shift\nshift out of range\n")
