// SPDX-License-Identifier: MIT
pragma solidity 0.8.15;

// Testing utilities
import { Bridge_Initializer } from "test/setup/Bridge_Initializer.sol";

// Libraries
import { Types } from "src/libraries/Types.sol";
import { Types as ITypes } from "src/L2/interfaces/IBaseFeeVault.sol";

// Test the implementations of the FeeVault
contract FeeVault_Test is Bridge_Initializer {
    /// @dev Tests that the constructor sets the correct values.
    function test_constructor_baseFeeVault_succeeds() external view {
        assertEq(baseFeeVault.RECIPIENT(), deploy.cfg().baseFeeVaultRecipient());
        assertEq(baseFeeVault.recipient(), deploy.cfg().baseFeeVaultRecipient());
        assertEq(baseFeeVault.MIN_WITHDRAWAL_AMOUNT(), deploy.cfg().baseFeeVaultMinimumWithdrawalAmount());
        assertEq(baseFeeVault.minWithdrawalAmount(), deploy.cfg().baseFeeVaultMinimumWithdrawalAmount());
        assertEq(ITypes.WithdrawalNetwork.unwrap(baseFeeVault.WITHDRAWAL_NETWORK()), uint8(Types.WithdrawalNetwork.L1));
        assertEq(ITypes.WithdrawalNetwork.unwrap(baseFeeVault.withdrawalNetwork()), uint8(Types.WithdrawalNetwork.L1));
    }
}
