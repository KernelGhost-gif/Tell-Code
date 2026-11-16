cmake_minimum_required(VERSION 3.16)
project(TellC LANGUAGES C CXX)
find_package(LLVM REQUIRED CONFIG)
message(STATUS "Using LLVM ${LLVM_PACKAGE_VERSION} from ${LLVM_DIR}")
set(TELL_LLC_PATH "${LLVM_TOOLS_BINARY_DIR}/llc" CACHE STRING
    "Path or command name for the llc matching the configured LLVM version")
llvm_map_components_to_libnames(TELL_LLVM_LIBS Core Support TargetParser)
add_library(tell_parser src/parser.cpp)
target_include_directories(tell_parser PUBLIC include)
target_compile_features(tell_parser PUBLIC cxx_std_17)
add_executable(tellc src/main.cpp src/codegen.cpp)
target_include_directories(tellc SYSTEM PRIVATE ${LLVM_INCLUDE_DIRS})
target_compile_definitions(tellc PRIVATE ${LLVM_DEFINITIONS} TELL_LLC_PATH="${TELL_LLC_PATH}")
target_link_libraries(tellc PRIVATE tell_parser ${TELL_LLVM_LIBS})
include(CTest)
if(BUILD_TESTING)
  add_executable(tell_parser_recursion_test tests/recursion.cpp)
  target_link_libraries(tell_parser_recursion_test PRIVATE tell_parser)
  add_test(NAME recursion_ast COMMAND tell_parser_recursion_test)
  add_executable(tell_parser_foreign_test tests/foreign.cpp)
  target_link_libraries(tell_parser_foreign_test PRIVATE tell_parser)
  add_test(NAME foreign_ast COMMAND tell_parser_foreign_test)
  add_executable(tell_parser_objects_test tests/objects.cpp)
  target_link_libraries(tell_parser_objects_test PRIVATE tell_parser)
  add_test(NAME object_ast COMMAND tell_parser_objects_test)
  add_test(
    NAME compiler_integration
    COMMAND ${CMAKE_COMMAND}
      "-DTELL_COMPILER=$<TARGET_FILE:tellc>"
      "-DTELL_SOURCE_DIR=${CMAKE_CURRENT_SOURCE_DIR}"
      "-DTELL_WORK_DIR=${CMAKE_CURRENT_BINARY_DIR}/integration"
      -P "${CMAKE_CURRENT_SOURCE_DIR}/tests/compiler_integration.cmake"
  )
endif()
