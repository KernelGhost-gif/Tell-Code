#pragma once

#include <llvm/ADT/DenseMap.h>
#include <llvm/IR/IRBuilder.h>
#include <llvm/IR/Module.h>

namespace tell {

// Emits the hashmap operations into the generated executable. Keys are passed
// as 64-bit integers or encoded text pointers, and values occupy one typed slot.
class HashmapCodegen {
public:
    HashmapCodegen(llvm::Module& module, llvm::IRBuilder<>& ir);

    llvm::Value* create();
    void cleanup();
    llvm::Value* length(llvm::Value* table);
    llvm::Value* find(llvm::Value* table, llvm::Value* key, bool text_key);
    llvm::Value* insert(llvm::Value* table, llvm::Value* key, bool text_key);
    llvm::Value* remove(llvm::Value* table, llvm::Value* key, bool text_key);
    llvm::Value* value_address(llvm::Value* entry);
    void copy_keys(llvm::Value* table, llvm::Value* destination, llvm::Type* key_type);

private:
    llvm::Module& module;
    llvm::IRBuilder<>& ir;
    llvm::LLVMContext& context;
    llvm::StructType* table_type;
    llvm::StructType* entry_type;
    llvm::GlobalVariable* registry_head = nullptr;
    llvm::Function* create_fn = nullptr;
    llvm::Function* cleanup_fn = nullptr;
    llvm::Function* text_hash_fn = nullptr;
    llvm::Function* find_link_fn = nullptr;
    llvm::Function* grow_fn = nullptr;
    llvm::Function* insert_fn = nullptr;
    llvm::DenseMap<llvm::Type*, llvm::Function*> copy_keys_fns;

    llvm::Value* hash(llvm::Value* key, bool text_key);
    llvm::Function* text_hash();
    llvm::Function* find_link();
    llvm::Function* grow();
    llvm::Function* insert_entry();
    llvm::Function* helper(llvm::StringRef name, llvm::Type* result, llvm::ArrayRef<llvm::Type*> args);
    llvm::Value* bucket(llvm::Value* table, llvm::Value* hash);
    llvm::Value* size_of(llvm::Type* type);
    llvm::Value* allocate(llvm::Type* type);
    llvm::Value* allocate_buckets(llvm::Value* capacity);
    void release(llvm::Value* address);
    template<class Visit>
    void scan_entries(llvm::Value* buckets, llvm::Value* capacity, Visit visit);
};

} // namespace tell
