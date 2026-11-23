if(NOT DEFINED TELL_COMPILER OR NOT DEFINED TELL_SOURCE_DIR OR NOT DEFINED TELL_WORK_DIR)
  message(FATAL_ERROR "TELL_COMPILER, TELL_SOURCE_DIR, and TELL_WORK_DIR are required")
endif()

file(MAKE_DIRECTORY "${TELL_WORK_DIR}")
if(WIN32)
  set(TELL_EXECUTABLE_SUFFIX ".exe")
else()
  set(TELL_EXECUTABLE_SUFFIX "")
endif()

function(compile_and_run name source expected_stdout)
  set(executable "${TELL_WORK_DIR}/${name}${TELL_EXECUTABLE_SUFFIX}")
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
    ${ARGN}
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
  set(executable "${TELL_WORK_DIR}/${name}${TELL_EXECUTABLE_SUFFIX}")
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
execute_process(COMMAND "${TELL_COMPILER}" "${TELL_SOURCE_DIR}/tests/fixtures/collection_bounds.tell" -o "${TELL_WORK_DIR}/collection_bounds${TELL_EXECUTABLE_SUFFIX}" RESULT_VARIABLE bounds_compile ERROR_VARIABLE bounds_error)
if(NOT bounds_compile EQUAL 0)
  message(FATAL_ERROR "Bounds fixture did not compile: ${bounds_error}")
endif()
execute_process(COMMAND "${TELL_WORK_DIR}/collection_bounds${TELL_EXECUTABLE_SUFFIX}" RESULT_VARIABLE bounds_result OUTPUT_QUIET ERROR_QUIET)
if(bounds_result STREQUAL "0")
  message(FATAL_ERROR "Out-of-bounds indexing should terminate the program")
endif()
compile_and_run(loops_assignment tests/fixtures/loops_assignment.tell "1\n3\n3\n1\n9\n8\n")
expect_compile_failure(assign_constant tests/fixtures/assign_constant.tell "cannot assign to constant 'value'")
expect_compile_failure(assign_type tests/fixtures/assign_type.tell "expected 'number', got 'text'")
expect_compile_failure(assign_index_type tests/fixtures/assign_index_type.tell "expected 'number', got 'text'")
execute_process(COMMAND "${TELL_COMPILER}" "${TELL_SOURCE_DIR}/tests/fixtures/io_errors.tell" -o "${TELL_WORK_DIR}/io_errors${TELL_EXECUTABLE_SUFFIX}" RESULT_VARIABLE io_compile ERROR_VARIABLE io_compile_error)
if(NOT io_compile EQUAL 0)
  message(FATAL_ERROR "I/O fixture failed to compile: ${io_compile_error}")
endif()
file(WRITE "${TELL_WORK_DIR}/io_input.txt" "42\r\n")
execute_process(COMMAND "${TELL_WORK_DIR}/io_errors${TELL_EXECUTABLE_SUFFIX}" WORKING_DIRECTORY "${TELL_WORK_DIR}" INPUT_FILE "${TELL_WORK_DIR}/io_input.txt" RESULT_VARIABLE io_result OUTPUT_VARIABLE io_output ERROR_VARIABLE io_error)
if(NOT io_result EQUAL 0 OR NOT io_output STREQUAL "Value? 42\nhello world\ncustom error\ncannot open file\nindex out of bounds\n2.5\n")
  message(FATAL_ERROR "I/O valid input failed: ${io_result}, [${io_output}], [${io_error}]")
endif()
file(WRITE "${TELL_WORK_DIR}/io_input.txt" "9223372036854775808\n")
execute_process(COMMAND "${TELL_WORK_DIR}/io_errors${TELL_EXECUTABLE_SUFFIX}" WORKING_DIRECTORY "${TELL_WORK_DIR}" INPUT_FILE "${TELL_WORK_DIR}/io_input.txt" RESULT_VARIABLE io_result OUTPUT_VARIABLE io_output ERROR_VARIABLE io_error)
if(NOT io_result EQUAL 0 OR NOT io_output STREQUAL "Value? invalid number\nhello world\ncustom error\ncannot open file\nindex out of bounds\n2.5\n")
  message(FATAL_ERROR "I/O invalid input failed: ${io_result}, [${io_output}], [${io_error}]")
endif()
file(WRITE "${TELL_WORK_DIR}/io_input.txt" "")
execute_process(COMMAND "${TELL_WORK_DIR}/io_errors${TELL_EXECUTABLE_SUFFIX}" WORKING_DIRECTORY "${TELL_WORK_DIR}" INPUT_FILE "${TELL_WORK_DIR}/io_input.txt" RESULT_VARIABLE io_result OUTPUT_VARIABLE io_output ERROR_VARIABLE io_error)
if(NOT io_result EQUAL 0 OR NOT io_output STREQUAL "Value? cannot read input\nhello world\ncustom error\ncannot open file\nindex out of bounds\n2.5\n")
  message(FATAL_ERROR "I/O EOF input failed: ${io_result}, [${io_output}], [${io_error}]")
endif()
execute_process(COMMAND "${TELL_COMPILER}" "${TELL_SOURCE_DIR}/tests/fixtures/uncaught_error.tell" -o "${TELL_WORK_DIR}/uncaught_error${TELL_EXECUTABLE_SUFFIX}" RESULT_VARIABLE uncaught_compile ERROR_VARIABLE uncaught_compile_error)
if(NOT uncaught_compile EQUAL 0)
  message(FATAL_ERROR "Uncaught error fixture failed to compile: ${uncaught_compile_error}")
endif()
execute_process(COMMAND "${TELL_WORK_DIR}/uncaught_error${TELL_EXECUTABLE_SUFFIX}" RESULT_VARIABLE uncaught_result OUTPUT_VARIABLE uncaught_output ERROR_VARIABLE uncaught_error)
if(NOT uncaught_result EQUAL 1 OR NOT uncaught_output STREQUAL "" OR NOT uncaught_error STREQUAL "Error: invalid number\n")
  message(FATAL_ERROR "Unexpected uncaught error: ${uncaught_result}, [${uncaught_output}], [${uncaught_error}]")
endif()
compile_and_run(mutable_collections tests/fixtures/mutable_collections.tell "5\n7\n9\n5\n2\n7\n2\n3\n4\n9\n8\ntwo\nitem not found in list\n2\n1\n3\n2\nfalse\n")
expect_compile_failure(mutable_type tests/fixtures/mutable_type.tell "expected 'number', got 'text'")
expect_compile_failure(mutable_target tests/fixtures/mutable_target.tell "add and remove require a list or vector")
compile_and_run(operators tests/fixtures/operators.tell "2\n7\n5\n-6\n16\n-2\n13\ntrue\nfalse\ntrue\ntrue\nfalse\ntrue\ninvalid shift\nshift out of range\n")
file(WRITE "${TELL_WORK_DIR}/hashmap_input.txt" "Ada\n")
compile_and_run(hashmaps tests/fixtures/hashmaps.tell "5\n2\ntrue\nfalse\n7\nfalse\n2\n3\nkey not found in hashmap\nkey not found in hashmap\n2.5\n9.5\n1\n2\ntrue\n512\n0\n0\n19\n"
  INPUT_FILE "${TELL_WORK_DIR}/hashmap_input.txt")
expect_compile_failure(hashmap_key_type tests/fixtures/hashmap_key_type.tell "expected 'text', got 'number'")
expect_compile_failure(hashmap_value_type tests/fixtures/hashmap_value_type.tell "expected 'number', got 'bool'")
# Exercise cleanup when a map variable is repeatedly overwritten and when
# nested maps are shared through aliases.
compile_and_run(hashmap_cleanup tests/fixtures/hashmap_cleanup.tell "2048\n21\n")

set(hashmap_cleanup_error_executable "${TELL_WORK_DIR}/hashmap_cleanup_error${TELL_EXECUTABLE_SUFFIX}")
execute_process(
  COMMAND "${TELL_COMPILER}" "${TELL_SOURCE_DIR}/tests/fixtures/hashmap_cleanup_error.tell"
    -o "${hashmap_cleanup_error_executable}"
  RESULT_VARIABLE error_compile_result
  ERROR_VARIABLE error_compile_stderr
)
if(NOT error_compile_result EQUAL 0)
  message(FATAL_ERROR "Could not compile hashmap cleanup error fixture: ${error_compile_stderr}")
endif()
execute_process(
  COMMAND "${hashmap_cleanup_error_executable}"
  RESULT_VARIABLE hashmap_error_status
  OUTPUT_VARIABLE hashmap_error_stdout
  ERROR_VARIABLE hashmap_error_stderr
)
if(NOT "${hashmap_error_status}" STREQUAL "1"
   OR NOT "${hashmap_error_stdout}" STREQUAL ""
   OR NOT "${hashmap_error_stderr}" STREQUAL "Error: intentional hashmap cleanup error\n")
  message(FATAL_ERROR
    "Unexpected hashmap error result (exit ${hashmap_error_status}).\nstdout:\n${hashmap_error_stdout}\nstderr:\n${hashmap_error_stderr}")
endif()

# Linux CI installs Valgrind and runs these map-only fixtures through Memcheck.
# It checks that tables, bucket arrays, and remaining entries are reclaimed.
find_program(TELL_VALGRIND_EXECUTABLE NAMES valgrind)
if(TELL_VALGRIND_EXECUTABLE)
  set(hashmap_cleanup_executable "${TELL_WORK_DIR}/hashmap_cleanup${TELL_EXECUTABLE_SUFFIX}")
  execute_process(
    COMMAND "${TELL_VALGRIND_EXECUTABLE}"
      --quiet
      --leak-check=full
      --show-leak-kinds=definite,indirect
      --errors-for-leak-kinds=definite,indirect
      --error-exitcode=99
      "${hashmap_cleanup_executable}"
    RESULT_VARIABLE valgrind_result
    OUTPUT_VARIABLE valgrind_stdout
    ERROR_VARIABLE valgrind_stderr
  )
  if(NOT valgrind_result EQUAL 0 OR NOT "${valgrind_stdout}" STREQUAL "2048\n21\n")
    message(FATAL_ERROR
      "Hashmap cleanup failed Memcheck (exit ${valgrind_result}).\nstdout:\n${valgrind_stdout}\nstderr:\n${valgrind_stderr}")
  endif()
  message(STATUS "PASS: hashmap allocations released at process exit (Valgrind)")

  execute_process(
    COMMAND "${TELL_VALGRIND_EXECUTABLE}"
      --quiet
      --leak-check=full
      --show-leak-kinds=definite,indirect
      --errors-for-leak-kinds=definite,indirect
      --error-exitcode=99
      "${hashmap_cleanup_error_executable}"
    RESULT_VARIABLE error_valgrind_result
    OUTPUT_VARIABLE error_valgrind_stdout
    ERROR_VARIABLE error_valgrind_stderr
  )
  if(NOT error_valgrind_result EQUAL 1
     OR NOT "${error_valgrind_stdout}" STREQUAL ""
     OR NOT "${error_valgrind_stderr}" MATCHES "Error: intentional hashmap cleanup error")
    message(FATAL_ERROR
      "Hashmap cleanup on uncaught error failed Memcheck (exit ${error_valgrind_result}).\nstdout:\n${error_valgrind_stdout}\nstderr:\n${error_valgrind_stderr}")
  endif()
  message(STATUS "PASS: hashmap allocations released on uncaught errors (Valgrind)")
endif()
