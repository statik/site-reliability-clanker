output "rwx_storage_class" {
  description = "Name of the StorageClass. Its volume supports ReadWriteMany."
  value       = kubernetes_storage_class_v1.rwx.metadata[0].name

  # A claim cannot bind until the volume exists.
  depends_on = [kubernetes_persistent_volume_v1.shared]
}
