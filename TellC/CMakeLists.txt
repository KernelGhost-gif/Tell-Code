cmake_minimum_required(VERSION 3.16)
project(TellC LANGUAGES C CXX)

find_package(LLVM REQUIRED CONFIG)
message(STATUS "Using LLVM ${LLVM_PACKAGE_VERSION} from ${LLVM_DIR}")

set(TELL_LLC_PATH "${LLVM_TOOLS_BINARY_DIR}/llc" CACHE STRING
    "Path or command name for the llc matching the configured LLVM version")

# The embedded LLD backend is currently supported only on Apple silicon.
# Intel Macs keep using the external clang/llc toolchain by default.
set(_tell_standalone_default OFF)
if(APPLE AND CMAKE_SYSTEM_PROCESSOR MATCHES "^(arm64|aarch64)$")
  set(_tell_standalone_default ON)
endif()
option(TELL_STANDALONE_MACOS "Embed LLVM code generation and LLD for Apple silicon macOS" ${_tell_standalone_default})
unset(_tell_standalone_default)

if(TELL_STANDALONE_MACOS)
  if(NOT APPLE)
    message(FATAL_ERROR "TELL_STANDALONE_MACOS requires macOS")
  endif()
  if(NOT CMAKE_SYSTEM_PROCESSOR MATCHES "^(arm64|aarch64)$")
    message(FATAL_ERROR "Standalone TellC currently supports Apple silicon only; disable TELL_STANDALONE_MACOS to use clang and llc")
  endif()

  set(LLD_SOURCE_DIR "" CACHE PATH "Path to the matching LLVM lld source directory")
  set(LLD_BUILD_DIR "" CACHE PATH "Path to the matching standalone lld build")
  if(NOT EXISTS "${LLD_BUILD_DIR}/lib/liblldMachO.a"
     OR NOT EXISTS "${LLD_BUILD_DIR}/lib/liblldCommon.a"
     OR NOT EXISTS "${LLD_SOURCE_DIR}/include/lld/Common/Driver.h")
    message(FATAL_ERROR
      "Set LLD_SOURCE_DIR and LLD_BUILD_DIR to matching LLVM lld sources and build (liblldMachO.a and liblldCommon.a are required)")
  endif()

  llvm_map_components_to_libnames(TELL_LLVM_LIBS
    nativecodegen AArch64AsmParser BinaryFormat BitReader BitWriter CGData
    Core DebugInfoDWARF Demangle LTO MC ObjCARCOpts Object Option Passes
    ProfileData Support TargetParser TextAPI)
else()
  llvm_map_components_to_libnames(TELL_LLVM_LIBS Core Support TargetParser)
endif()

add_library(tell_parser src/parser.cpp)
target_include_directories(tell_parser PUBLIC include)
target_compile_features(tell_parser PUBLIC cxx_std_17)

add_executable(tellc src/main.cpp src/codegen.cpp)
target_compile_features(tellc PRIVATE cxx_std_17)
target_include_directories(tellc SYSTEM PRIVATE ${LLVM_INCLUDE_DIRS})
target_compile_definitions(tellc PRIVATE ${LLVM_DEFINITIONS} TELL_LLC_PATH="${TELL_LLC_PATH}")

if(TELL_STANDALONE_MACOS)
  target_compile_definitions(tellc PRIVATE TELL_STANDALONE_MACOS)
  target_include_directories(tellc SYSTEM PRIVATE "${LLD_SOURCE_DIR}/include")

  # Keep an escape hatch for custom LLVM builds that still expose shared zstd.
  set(TELL_ZSTD_STATIC_LIBRARY "" CACHE FILEPATH "Static zstd library used by LLVM")
  if(TARGET zstd::libzstd_shared)
    if(NOT EXISTS "${TELL_ZSTD_STATIC_LIBRARY}" OR NOT TELL_ZSTD_STATIC_LIBRARY MATCHES "\\.a$")
      message(FATAL_ERROR
        "LLVM uses shared zstd; provide its static archive in TELL_ZSTD_STATIC_LIBRARY or rebuild LLVM with LLVM_ENABLE_ZSTD=OFF")
    endif()
    set_target_properties(zstd::libzstd_shared PROPERTIES IMPORTED_LOCATION "${TELL_ZSTD_STATIC_LIBRARY}")
  endif()

  target_link_libraries(tellc PRIVATE
    tell_parser
    "${LLD_BUILD_DIR}/lib/liblldMachO.a"
    "${LLD_BUILD_DIR}/lib/liblldCommon.a"
    ${TELL_LLVM_LIBS})
else()
  target_link_libraries(tellc PRIVATE tell_parser ${TELL_LLVM_LIBS})
endif()

include(CTest)
if(BUILD_TESTING)
  find_package(Python3 REQUIRED COMPONENTS Interpreter)
  add_test(NAME cli_smoke
    COMMAND "${Python3_EXECUTABLE}"
      "${CMAKE_CURRENT_SOURCE_DIR}/tests/cli_smoke.py"
      "$<TARGET_FILE:tellc>"
      "${CMAKE_CURRENT_SOURCE_DIR}/examples/factorial.tell")

  if(TELL_STANDALONE_MACOS)
    add_test(NAME standalone_macos
      COMMAND "${Python3_EXECUTABLE}"
        "${CMAKE_CURRENT_SOURCE_DIR}/tests/standalone_macos.py"
        "$<TARGET_FILE:tellc>"
        "${CMAKE_CURRENT_SOURCE_DIR}/examples/objects.tell")
    add_library(tell_test_cpp_static STATIC tests/cpp_library.cpp)
    add_library(tell_test_cpp_dynamic SHARED tests/cpp_library.cpp)
    set_target_properties(tell_test_cpp_dynamic PROPERTIES BUILD_WITH_INSTALL_NAME_DIR ON INSTALL_NAME_DIR "${CMAKE_CURRENT_BINARY_DIR}")
    add_test(NAME cpp_integration COMMAND "${Python3_EXECUTABLE}"
      "${CMAKE_CURRENT_SOURCE_DIR}/tests/cpp_integration.py" "$<TARGET_FILE:tellc>"
      "$<TARGET_FILE:tell_test_cpp_static>" "$<TARGET_FILE:tell_test_cpp_dynamic>")
  endif()

  add_executable(tell_parser_recursion_test tests/recursion.cpp)
  target_link_libraries(tell_parser_recursion_test PRIVATE tell_parser)
  add_test(NAME recursion_ast COMMAND tell_parser_recursion_test)

  add_executable(tell_parser_foreign_test tests/foreign.cpp)
  target_link_libraries(tell_parser_foreign_test PRIVATE tell_parser)
  add_test(NAME foreign_ast COMMAND tell_parser_foreign_test)

  add_executable(tell_parser_objects_test tests/objects.cpp)
  target_link_libraries(tell_parser_objects_test PRIVATE tell_parser)
  add_test(NAME object_ast COMMAND tell_parser_objects_test)

  add_test(NAME compiler_integration
    COMMAND ${CMAKE_COMMAND}
      "-DTELL_COMPILER=$<TARGET_FILE:tellc>"
      "-DTELL_SOURCE_DIR=${CMAKE_CURRENT_SOURCE_DIR}"
      "-DTELL_WORK_DIR=${CMAKE_CURRENT_BINARY_DIR}/integration"
      -P "${CMAKE_CURRENT_SOURCE_DIR}/tests/compiler_integration.cmake")
endif()
