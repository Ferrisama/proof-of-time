include!(concat!(env!("OUT_DIR"), "/methods.rs"));

// Create aliases for convenience
pub use METHOD_ELF as VDF_GUEST_ELF;
pub use METHOD_ID as VDF_GUEST_ID;
