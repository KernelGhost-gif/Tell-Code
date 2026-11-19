#include "hashmap_codegen.hpp"

#include <limits>

namespace tell {
namespace {
enum TableField { Length, Capacity, Buckets, TrackedNext };
enum EntryField { Hash, Key, Value, Next };
constexpr uint64_t initial_capacity = 8;
}

HashmapCodegen::HashmapCodegen(llvm::Module& module, llvm::IRBuilder<>& ir)
    : module(module), ir(ir), context(module.getContext()),
      table_type(llvm::StructType::get(context, {ir.getInt64Ty(), ir.getInt64Ty(), ir.getPtrTy(), ir.getPtrTy()})),
      entry_type(llvm::StructType::get(context, {ir.getInt64Ty(), ir.getInt64Ty(), ir.getInt64Ty(), ir.getPtrTy()})) {
    registry_head = new llvm::GlobalVariable(
        module, ir.getPtrTy(), false, llvm::GlobalValue::InternalLinkage,
        llvm::ConstantPointerNull::get(ir.getPtrTy()), "tell.hashmap.registry");
}

llvm::Function* HashmapCodegen::helper(llvm::StringRef name, llvm::Type* result,
                                       llvm::ArrayRef<llvm::Type*> args) {
    auto type = llvm::FunctionType::get(result, args, false);
    auto fn = llvm::Function::Create(type, llvm::Function::InternalLinkage, name, module);
    ir.SetInsertPoint(llvm::BasicBlock::Create(context, "entry", fn));
    return fn;
}

llvm::Value* HashmapCodegen::size_of(llvm::Type* type) {
    auto null = llvm::ConstantPointerNull::get(ir.getPtrTy());
    return ir.CreatePtrToInt(ir.CreateGEP(type, null, ir.getInt64(1)), ir.getInt64Ty());
}

llvm::Value* HashmapCodegen::allocate(llvm::Type* type) {
    auto signature = llvm::FunctionType::get(ir.getPtrTy(), {ir.getInt64Ty()}, false);
    return ir.CreateCall(module.getOrInsertFunction("malloc", signature), {size_of(type)});
}

llvm::Value* HashmapCodegen::allocate_buckets(llvm::Value* capacity) {
    auto signature = llvm::FunctionType::get(ir.getPtrTy(), {ir.getInt64Ty(), ir.getInt64Ty()}, false);
    return ir.CreateCall(module.getOrInsertFunction("calloc", signature), {capacity, size_of(ir.getPtrTy())});
}

void HashmapCodegen::release(llvm::Value* address) {
    auto signature = llvm::FunctionType::get(ir.getVoidTy(), {ir.getPtrTy()}, false);
    ir.CreateCall(module.getOrInsertFunction("free", signature), {address});
}

llvm::Value* HashmapCodegen::create() {
    if (!create_fn) {
        llvm::IRBuilderBase::InsertPointGuard guard(ir);
        auto fn = create_fn = helper("tell.hashmap.create", ir.getPtrTy(), {});
        auto allocate_data = llvm::BasicBlock::Create(context, "allocate.buckets", fn);
        auto ready = llvm::BasicBlock::Create(context, "ready", fn);
        auto release_table = llvm::BasicBlock::Create(context, "release.table", fn);
        auto failed = llvm::BasicBlock::Create(context, "failed", fn);
        auto table = allocate(table_type);
        ir.CreateCondBr(ir.CreateIsNull(table), failed, allocate_data);
        ir.SetInsertPoint(allocate_data);
        auto capacity = ir.getInt64(initial_capacity);
        auto buckets = allocate_buckets(capacity);
        ir.CreateCondBr(ir.CreateIsNull(buckets), release_table, ready);
        ir.SetInsertPoint(ready);
        ir.CreateStore(ir.getInt64(0), ir.CreateStructGEP(table_type, table, Length));
        ir.CreateStore(capacity, ir.CreateStructGEP(table_type, table, Capacity));
        ir.CreateStore(buckets, ir.CreateStructGEP(table_type, table, Buckets));
        // The table itself is the registry node, so aliases and nested maps
        // register exactly once without a second allocation.
        ir.CreateStore(ir.CreateLoad(ir.getPtrTy(), registry_head),
                       ir.CreateStructGEP(table_type, table, TrackedNext));
        ir.CreateStore(table, registry_head);
        ir.CreateRet(table);
        ir.SetInsertPoint(release_table);
        release(table);
        ir.CreateBr(failed);
        ir.SetInsertPoint(failed);
        ir.CreateRet(llvm::ConstantPointerNull::get(ir.getPtrTy()));
    }
    return ir.CreateCall(create_fn);
}

llvm::Value* HashmapCodegen::length(llvm::Value* table) {
    return ir.CreateLoad(ir.getInt64Ty(), ir.CreateStructGEP(table_type, table, Length));
}

void HashmapCodegen::cleanup() {
    if (!cleanup_fn) {
        llvm::IRBuilderBase::InsertPointGuard guard(ir);
        auto ptr = ir.getPtrTy();
        auto fn = cleanup_fn = helper("tell.hashmap.cleanup", ir.getVoidTy(), {});
        auto start = ir.GetInsertBlock();
        auto loop = llvm::BasicBlock::Create(context, "hashmap.cleanup.loop", fn);
        auto release_map = llvm::BasicBlock::Create(context, "hashmap.cleanup.map", fn);
        auto done = llvm::BasicBlock::Create(context, "hashmap.cleanup.done", fn);

        auto first_table = ir.CreateLoad(ptr, registry_head);
        ir.CreateBr(loop);
        ir.SetInsertPoint(loop);
        auto table = ir.CreatePHI(ptr, 2, "current.map");
        table->addIncoming(first_table, start);
        ir.CreateCondBr(ir.CreateIsNull(table), done, release_map);

        ir.SetInsertPoint(release_map);
        // Capture the next registry node before freeing this table.
        auto next_table = ir.CreateLoad(ptr, ir.CreateStructGEP(table_type, table, TrackedNext));
        auto capacity = ir.CreateLoad(ir.getInt64Ty(), ir.CreateStructGEP(table_type, table, Capacity));
        auto buckets = ir.CreateLoad(ptr, ir.CreateStructGEP(table_type, table, Buckets));

        // scan_entries loads each next pointer before freeing the current entry.
        scan_entries(buckets, capacity, [&](llvm::Value* entry, llvm::Value*) {
            release(entry);
        });
        auto cleanup_block = ir.GetInsertBlock();
        release(buckets);
        release(table);
        ir.CreateBr(loop);
        table->addIncoming(next_table, cleanup_block);

        ir.SetInsertPoint(done);
        ir.CreateStore(llvm::ConstantPointerNull::get(ptr), registry_head);
        ir.CreateRetVoid();
    }
    ir.CreateCall(cleanup_fn);
}

llvm::Value* HashmapCodegen::value_address(llvm::Value* entry) {
    return ir.CreateStructGEP(entry_type, entry, Value);
}

llvm::Value* HashmapCodegen::bucket(llvm::Value* table, llvm::Value* hash_value) {
    auto capacity = ir.CreateLoad(ir.getInt64Ty(), ir.CreateStructGEP(table_type, table, Capacity));
    auto buckets = ir.CreateLoad(ir.getPtrTy(), ir.CreateStructGEP(table_type, table, Buckets));
    auto index = ir.CreateAnd(hash_value, ir.CreateSub(capacity, ir.getInt64(1)));
    return ir.CreateGEP(ir.getPtrTy(), buckets, index);
}

llvm::Function* HashmapCodegen::text_hash() {
    if (text_hash_fn) return text_hash_fn;
    llvm::IRBuilderBase::InsertPointGuard guard(ir);
    auto fn = text_hash_fn = helper("tell.hashmap.hash_text", ir.getInt64Ty(), {ir.getPtrTy()});
    auto start = ir.GetInsertBlock();
    auto loop = llvm::BasicBlock::Create(context, "byte", fn);
    auto mix = llvm::BasicBlock::Create(context, "mix", fn);
    auto done = llvm::BasicBlock::Create(context, "done", fn);
    ir.CreateBr(loop);
    ir.SetInsertPoint(loop);
    auto cursor = ir.CreatePHI(ir.getPtrTy(), 2);
    auto hash_value = ir.CreatePHI(ir.getInt64Ty(), 2);
    cursor->addIncoming(fn->getArg(0), start);
    // FNV-1a over the same NUL-terminated bytes used by Tell text equality.
    hash_value->addIncoming(ir.getInt64(14695981039346656037ULL), start);
    auto byte = ir.CreateLoad(ir.getInt8Ty(), cursor);
    ir.CreateCondBr(ir.CreateICmpEQ(byte, ir.getInt8(0)), done, mix);
    ir.SetInsertPoint(mix);
    auto next = ir.CreateGEP(ir.getInt8Ty(), cursor, ir.getInt64(1));
    auto mixed = ir.CreateMul(ir.CreateXor(hash_value, ir.CreateZExt(byte, ir.getInt64Ty())),
                              ir.getInt64(1099511628211ULL));
    ir.CreateBr(loop);
    cursor->addIncoming(next, mix);
    hash_value->addIncoming(mixed, mix);
    ir.SetInsertPoint(done);
    ir.CreateRet(hash_value);
    return fn;
}

llvm::Value* HashmapCodegen::hash(llvm::Value* key, bool text_key) {
    auto value = text_key ? ir.CreateCall(text_hash(), {ir.CreateIntToPtr(key, ir.getPtrTy())})
                         : key;
    // MurmurHash3's finalizer distributes patterns in both integer and text keys
    // across the low bits used to select a bucket.
    value = ir.CreateXor(value, ir.CreateLShr(value, 33));
    value = ir.CreateMul(value, ir.getInt64(0xff51afd7ed558ccdULL));
    value = ir.CreateXor(value, ir.CreateLShr(value, 33));
    value = ir.CreateMul(value, ir.getInt64(0xc4ceb9fe1a85ec53ULL));
    return ir.CreateXor(value, ir.CreateLShr(value, 33));
}

llvm::Function* HashmapCodegen::find_link() {
    if (find_link_fn) return find_link_fn;
    llvm::IRBuilderBase::InsertPointGuard guard(ir);
    auto ptr = ir.getPtrTy();
    auto integer = ir.getInt64Ty();
    auto fn = find_link_fn = helper("tell.hashmap.find_link", ptr, {ptr, integer, integer, ir.getInt1Ty()});
    auto table = fn->getArg(0);
    auto hash_value = fn->getArg(1);
    auto key = fn->getArg(2);
    auto text_key = fn->getArg(3);
    auto start = ir.GetInsertBlock();
    auto loop = llvm::BasicBlock::Create(context, "link", fn);
    auto check_hash = llvm::BasicBlock::Create(context, "check.hash", fn);
    auto check_key = llvm::BasicBlock::Create(context, "check.key", fn);
    auto check_text = llvm::BasicBlock::Create(context, "check.text", fn);
    auto compare_text = llvm::BasicBlock::Create(context, "compare.text", fn);
    auto advance = llvm::BasicBlock::Create(context, "advance", fn);
    auto done = llvm::BasicBlock::Create(context, "done", fn);
    auto head = bucket(table, hash_value);
    ir.CreateBr(loop);
    ir.SetInsertPoint(loop);
    auto link = ir.CreatePHI(ptr, 2);
    link->addIncoming(head, start);
    auto entry = ir.CreateLoad(ptr, link);
    ir.CreateCondBr(ir.CreateIsNull(entry), done, check_hash);
    ir.SetInsertPoint(check_hash);
    auto entry_hash = ir.CreateLoad(integer, ir.CreateStructGEP(entry_type, entry, Hash));
    ir.CreateCondBr(ir.CreateICmpEQ(entry_hash, hash_value), check_key, advance);
    ir.SetInsertPoint(check_key);
    auto entry_key = ir.CreateLoad(integer, ir.CreateStructGEP(entry_type, entry, Key));
    ir.CreateCondBr(ir.CreateICmpEQ(entry_key, key), done, check_text);
    ir.SetInsertPoint(check_text);
    ir.CreateCondBr(text_key, compare_text, advance);
    ir.SetInsertPoint(compare_text);
    auto signature = llvm::FunctionType::get(ir.getInt32Ty(), {ptr, ptr}, false);
    auto comparison = ir.CreateCall(module.getOrInsertFunction("strcmp", signature),
                                    {ir.CreateIntToPtr(entry_key, ptr), ir.CreateIntToPtr(key, ptr)});
    ir.CreateCondBr(ir.CreateICmpEQ(comparison, ir.getInt32(0)), done, advance);
    ir.SetInsertPoint(advance);
    auto next = ir.CreateStructGEP(entry_type, entry, Next);
    ir.CreateBr(loop);
    link->addIncoming(next, advance);
    ir.SetInsertPoint(done);
    ir.CreateRet(link);
    return fn;
}

template<class Visit>
void HashmapCodegen::scan_entries(llvm::Value* buckets, llvm::Value* capacity, Visit visit) {
    auto ptr = ir.getPtrTy();
    auto start = ir.GetInsertBlock();
    auto fn = start->getParent();
    auto scan = llvm::BasicBlock::Create(context, "scan.buckets", fn);
    auto load_head = llvm::BasicBlock::Create(context, "load.head", fn);
    auto chain = llvm::BasicBlock::Create(context, "scan.chain", fn);
    auto step = llvm::BasicBlock::Create(context, "visit.entry", fn);
    auto advance = llvm::BasicBlock::Create(context, "next.bucket", fn);
    auto done = llvm::BasicBlock::Create(context, "scan.done", fn);
    ir.CreateBr(scan);
    ir.SetInsertPoint(scan);
    auto index = ir.CreatePHI(ir.getInt64Ty(), 2);
    index->addIncoming(ir.getInt64(0), start);
    ir.CreateCondBr(ir.CreateICmpULT(index, capacity), load_head, done);
    ir.SetInsertPoint(load_head);
    auto head = ir.CreateLoad(ptr, ir.CreateGEP(ptr, buckets, index));
    ir.CreateBr(chain);
    ir.SetInsertPoint(chain);
    auto entry = ir.CreatePHI(ptr, 2);
    entry->addIncoming(head, load_head);
    ir.CreateCondBr(ir.CreateIsNull(entry), advance, step);
    ir.SetInsertPoint(step);
    auto next_addr = ir.CreateStructGEP(entry_type, entry, Next);
    auto next = ir.CreateLoad(ptr, next_addr);
    visit(entry, next_addr);
    ir.CreateBr(chain);
    entry->addIncoming(next, step);
    ir.SetInsertPoint(advance);
    auto next_index = ir.CreateAdd(index, ir.getInt64(1));
    ir.CreateBr(scan);
    index->addIncoming(next_index, advance);
    ir.SetInsertPoint(done);
}

llvm::Function* HashmapCodegen::grow() {
    if (grow_fn) return grow_fn;
    llvm::IRBuilderBase::InsertPointGuard guard(ir);
    auto ptr = ir.getPtrTy();
    auto integer = ir.getInt64Ty();
    auto fn = grow_fn = helper("tell.hashmap.grow", ir.getInt1Ty(), {ptr});
    auto table = fn->getArg(0);
    auto capacity_addr = ir.CreateStructGEP(table_type, table, Capacity);
    auto buckets_addr = ir.CreateStructGEP(table_type, table, Buckets);
    auto capacity = ir.CreateLoad(integer, capacity_addr);
    auto old_buckets = ir.CreateLoad(ptr, buckets_addr);
    auto allocate_data = llvm::BasicBlock::Create(context, "allocate.buckets", fn);
    auto rehash = llvm::BasicBlock::Create(context, "rehash", fn);
    auto failed = llvm::BasicBlock::Create(context, "failed", fn);
    auto max_capacity = ir.CreateUDiv(ir.getInt64(std::numeric_limits<int64_t>::max() / 2), size_of(ptr));
    ir.CreateCondBr(ir.CreateICmpUGT(capacity, max_capacity), failed, allocate_data);
    ir.SetInsertPoint(allocate_data);
    auto new_capacity = ir.CreateMul(capacity, ir.getInt64(2));
    auto new_buckets = allocate_buckets(new_capacity);
    auto mask = ir.CreateSub(new_capacity, ir.getInt64(1));
    ir.CreateCondBr(ir.CreateIsNull(new_buckets), failed, rehash);

    // Relink the existing entries using their stored hashes. Only the bucket
    // array is replaced, so references to the table remain valid after growth.
    ir.SetInsertPoint(rehash);
    scan_entries(old_buckets, capacity, [&](llvm::Value* entry, llvm::Value* next_addr) {
        auto hash_value = ir.CreateLoad(integer, ir.CreateStructGEP(entry_type, entry, Hash));
        auto new_head = ir.CreateGEP(ptr, new_buckets, ir.CreateAnd(hash_value, mask));
        ir.CreateStore(ir.CreateLoad(ptr, new_head), next_addr);
        ir.CreateStore(entry, new_head);
    });
    ir.CreateStore(new_capacity, capacity_addr);
    ir.CreateStore(new_buckets, buckets_addr);
    release(old_buckets);
    ir.CreateRet(ir.getTrue());
    ir.SetInsertPoint(failed);
    ir.CreateRet(ir.getFalse());
    return fn;
}

llvm::Function* HashmapCodegen::insert_entry() {
    if (insert_fn) return insert_fn;
    llvm::IRBuilderBase::InsertPointGuard guard(ir);
    auto ptr = ir.getPtrTy();
    auto integer = ir.getInt64Ty();
    auto fn = insert_fn = helper("tell.hashmap.insert", ptr, {ptr, integer, integer, ir.getInt1Ty()});
    auto table = fn->getArg(0);
    auto hash_value = fn->getArg(1);
    auto key = fn->getArg(2);
    auto existing = llvm::BasicBlock::Create(context, "existing", fn);
    auto check_capacity = llvm::BasicBlock::Create(context, "check.capacity", fn);
    auto grow_table = llvm::BasicBlock::Create(context, "grow", fn);
    auto allocate_entry = llvm::BasicBlock::Create(context, "allocate.entry", fn);
    auto ready = llvm::BasicBlock::Create(context, "ready", fn);
    auto failed = llvm::BasicBlock::Create(context, "failed", fn);
    auto link = ir.CreateCall(find_link(), {table, hash_value, key, fn->getArg(3)});
    auto found = ir.CreateLoad(ptr, link);
    ir.CreateCondBr(ir.CreateIsNull(found), check_capacity, existing);
    ir.SetInsertPoint(existing);
    ir.CreateRet(found);
    ir.SetInsertPoint(check_capacity);
    auto length_addr = ir.CreateStructGEP(table_type, table, Length);
    auto count = ir.CreateLoad(integer, length_addr);
    auto capacity = ir.CreateLoad(integer, ir.CreateStructGEP(table_type, table, Capacity));
    ir.CreateCondBr(ir.CreateICmpEQ(count, capacity), grow_table, allocate_entry);
    ir.SetInsertPoint(grow_table);
    ir.CreateCondBr(ir.CreateCall(grow(), {table}), allocate_entry, failed);
    ir.SetInsertPoint(allocate_entry);
    auto entry = allocate(entry_type);
    ir.CreateCondBr(ir.CreateIsNull(entry), failed, ready);
    ir.SetInsertPoint(ready);
    auto head = bucket(table, hash_value);
    ir.CreateStore(hash_value, ir.CreateStructGEP(entry_type, entry, Hash));
    ir.CreateStore(key, ir.CreateStructGEP(entry_type, entry, Key));
    ir.CreateStore(ir.CreateLoad(ptr, head), ir.CreateStructGEP(entry_type, entry, Next));
    ir.CreateStore(entry, head);
    ir.CreateStore(ir.CreateAdd(count, ir.getInt64(1)), length_addr);
    ir.CreateRet(entry);
    ir.SetInsertPoint(failed);
    ir.CreateRet(llvm::ConstantPointerNull::get(ptr));
    return fn;
}

llvm::Value* HashmapCodegen::find(llvm::Value* table, llvm::Value* key, bool text_key) {
    auto link = ir.CreateCall(find_link(), {table, hash(key, text_key), key, ir.getInt1(text_key)});
    return ir.CreateLoad(ir.getPtrTy(), link);
}

llvm::Value* HashmapCodegen::insert(llvm::Value* table, llvm::Value* key, bool text_key) {
    return ir.CreateCall(insert_entry(), {table, hash(key, text_key), key, ir.getInt1(text_key)});
}

llvm::Value* HashmapCodegen::remove(llvm::Value* table, llvm::Value* key, bool text_key) {
    auto ptr = ir.getPtrTy();
    auto link = ir.CreateCall(find_link(), {table, hash(key, text_key), key, ir.getInt1(text_key)});
    auto entry = ir.CreateLoad(ptr, link);
    auto found = ir.CreateIsNotNull(entry);
    auto fn = ir.GetInsertBlock()->getParent();
    auto unlink = llvm::BasicBlock::Create(context, "hashmap.unlink", fn);
    auto done = llvm::BasicBlock::Create(context, "hashmap.remove.done", fn);
    ir.CreateCondBr(found, unlink, done);
    ir.SetInsertPoint(unlink);
    auto next = ir.CreateLoad(ptr, ir.CreateStructGEP(entry_type, entry, Next));
    ir.CreateStore(next, link);
    auto length_addr = ir.CreateStructGEP(table_type, table, Length);
    ir.CreateStore(ir.CreateSub(ir.CreateLoad(ir.getInt64Ty(), length_addr), ir.getInt64(1)), length_addr);
    release(entry);
    ir.CreateBr(done);
    ir.SetInsertPoint(done);
    return found;
}

void HashmapCodegen::copy_keys(llvm::Value* table, llvm::Value* destination, llvm::Type* key_type) {
    auto ptr = ir.getPtrTy();
    auto integer = ir.getInt64Ty();
    auto& copy = copy_keys_fns[key_type];
    if (!copy) {
        llvm::IRBuilderBase::InsertPointGuard guard(ir);
        copy = helper("tell.hashmap.copy_keys", ir.getVoidTy(), {ptr, ptr});
        auto source = copy->getArg(0);
        auto cursor = ir.CreateAlloca(ptr);
        ir.CreateStore(copy->getArg(1), cursor);
        auto capacity = ir.CreateLoad(integer, ir.CreateStructGEP(table_type, source, Capacity));
        auto buckets = ir.CreateLoad(ptr, ir.CreateStructGEP(table_type, source, Buckets));
        scan_entries(buckets, capacity, [&](llvm::Value* entry, llvm::Value*) {
            auto bits = ir.CreateLoad(integer, ir.CreateStructGEP(entry_type, entry, Key));
            auto key = key_type->isPointerTy() ? ir.CreateIntToPtr(bits, ptr) : ir.CreateZExtOrTrunc(bits, key_type);
            auto target = ir.CreateLoad(ptr, cursor);
            ir.CreateStore(key, target);
            ir.CreateStore(ir.CreateGEP(key_type, target, ir.getInt64(1)), cursor);
        });
        ir.CreateRetVoid();
    }
    ir.CreateCall(copy, {table, destination});
}

} // namespace tell
