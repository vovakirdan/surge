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
	size, err := vm.storageSizeOf(src.TypeID)
	if err != nil {
		return err
	}
	if _, err := src.resolve(size); err != nil {
		return err
	}
	if _, err := dst.resolve(size); err != nil {
		return err
	}
	if shape, err := vm.unionMembers(src.TypeID); err == nil {
		index, err := vm.storageActiveCase(src, shape)
		if err != nil {
			return err
		}
		for _, member := range shape.Cases[index].Payload {
			if err := vm.storageMoveMemberPreflight(dst, src, member); err != nil {
				return err
			}
		}
		return nil
	}
	members, err := vm.compositeMembers(src.TypeID)
	if err != nil {
		return err
	}
	for _, member := range members {
		if err := vm.storageMoveMemberPreflight(dst, src, member); err != nil {
			return err
		}
	}
	return nil
}

func (vm *VM) storageMoveMemberPreflight(dst, src StorageRef, member storageMember) error {
	dstMember, err := dst.memberRef(member)
	if err != nil {
		return err
	}
	srcMember, err := src.memberRef(member)
	if err != nil {
		return err
	}
	if member.Kind == cellComposite {
		return vm.storageMovePreflight(dstMember, srcMember)
	}
	if _, err := dstMember.resolve(member.Size); err != nil {
		return err
	}
	_, err = vm.storageReadCell(srcMember, member)
	return err
}

func (vm *VM) storageMoveWalk(dst, src StorageRef) error {
	if shape, err := vm.unionMembers(src.TypeID); err == nil {
		index, err := vm.storageActiveCase(src, shape)
		if err != nil {
			return err
		}
		if err := vm.storageSetActiveCase(dst, shape, index); err != nil {
			return err
		}
		for _, member := range shape.Cases[index].Payload {
			if err := vm.storageMoveMember(dst, src, member); err != nil {
				return err
			}
		}
		return vm.storageZero(src)
	}
	members, err := vm.compositeMembers(src.TypeID)
	if err != nil {
		return err
	}
	for _, member := range members {
		if err := vm.storageMoveMember(dst, src, member); err != nil {
			return err
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
	value, err := vm.storageReadCell(src, member)
	if err != nil {
		return err
	}
	if _, err := dst.resolve(member.Size); err != nil {
		return err
	}
	if err := vm.storageWriteCell(dst, member, value); err != nil {
		return err
	}
	bytes, err := src.resolve(member.Size)
	if err != nil {
		return err
	}
	clear(bytes)
	return nil
}
