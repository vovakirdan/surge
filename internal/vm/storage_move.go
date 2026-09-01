package vm

import "fmt"

// storageMoveInit transfers one exact value between disjoint extents without
// cloning any leaf. A complete preflight runs before either extent is changed;
// the second walk contains only operations preflight proved valid.
func (vm *VM) storageMoveInit(dst, src StorageRef) error {
	proceed, err := vm.copyPreflight(dst, src)
	if err != nil || !proceed {
		return err
	}
	if err := vm.storageMovePreflight(dst, src); err != nil {
		return err
	}
	if err := vm.storageZero(dst); err != nil {
		return err
	}
	return vm.storageMoveWalk(dst, src)
}

func (vm *VM) storageMovePreflight(dst, src StorageRef) error {
	size, sizeErr := vm.storageSizeOf(src.TypeID)
	if sizeErr != nil {
		return sizeErr
	}
	if _, resolveErr := src.resolve(size); resolveErr != nil {
		return resolveErr
	}
	if _, resolveErr := dst.resolve(size); resolveErr != nil {
		return resolveErr
	}
	if shape, unionErr := vm.unionMembers(src.TypeID); unionErr == nil {
		index, activeErr := vm.storageActiveCase(src, shape)
		if activeErr != nil {
			return activeErr
		}
		for _, member := range shape.Cases[index].Payload {
			if memberErr := vm.storageMoveMemberPreflight(dst, src, member); memberErr != nil {
				return memberErr
			}
		}
		return nil
	}
	members, membersErr := vm.compositeMembers(src.TypeID)
	if membersErr != nil {
		return membersErr
	}
	for _, member := range members {
		if memberErr := vm.storageMoveMemberPreflight(dst, src, member); memberErr != nil {
			return memberErr
		}
	}
	return nil
}

func (vm *VM) storageMoveMemberPreflight(dst, src StorageRef, member storageMember) error {
	dstMember, dstErr := dst.memberRef(member)
	if dstErr != nil {
		return dstErr
	}
	srcMember, srcErr := src.memberRef(member)
	if srcErr != nil {
		return srcErr
	}
	if member.Kind == cellComposite {
		return vm.storageMovePreflight(dstMember, srcMember)
	}
	if _, resolveErr := dstMember.resolve(member.Size); resolveErr != nil {
		return resolveErr
	}
	_, readErr := vm.storageReadCell(srcMember, member)
	return readErr
}

func (vm *VM) storageMoveWalk(dst, src StorageRef) error {
	if shape, unionErr := vm.unionMembers(src.TypeID); unionErr == nil {
		index, activeErr := vm.storageActiveCase(src, shape)
		if activeErr != nil {
			return activeErr
		}
		if activateErr := vm.storageSetActiveCase(dst, shape, index); activateErr != nil {
			return activateErr
		}
		for _, member := range shape.Cases[index].Payload {
			if memberErr := vm.storageMoveMember(dst, src, member); memberErr != nil {
				return memberErr
			}
		}
		return vm.storageZero(src)
	}
	members, membersErr := vm.compositeMembers(src.TypeID)
	if membersErr != nil {
		return membersErr
	}
	for _, member := range members {
		if memberErr := vm.storageMoveMember(dst, src, member); memberErr != nil {
			return memberErr
		}
	}
	return vm.storageZero(src)
}

func (vm *VM) storageMoveMember(dst, src StorageRef, member storageMember) error {
	dstMember, err := dst.memberRef(member)
	if err != nil {
		return err
	}
	srcMember, err := src.memberRef(member)
	if err != nil {
		return err
	}
	if member.Kind == cellComposite {
		return vm.storageMoveWalk(dstMember, srcMember)
	}
	return vm.storageMoveCellInit(dstMember, srcMember, member)
}

func (vm *VM) storageMoveCellInit(dst, src StorageRef, member storageMember) error {
	if member.Kind == cellComposite {
		return fmt.Errorf("storage: composite type#%d needs a transfer walk", member.TypeID)
	}
	value, readErr := vm.storageReadCell(src, member)
	if readErr != nil {
		return readErr
	}
	if _, resolveErr := dst.resolve(member.Size); resolveErr != nil {
		return resolveErr
	}
	if writeErr := vm.storageWriteCell(dst, member, value); writeErr != nil {
		return writeErr
	}
	bytes, resolveErr := src.resolve(member.Size)
	if resolveErr != nil {
		return resolveErr
	}
	clear(bytes)
	return nil
}
