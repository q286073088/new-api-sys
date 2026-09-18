import { createFileRoute, redirect } from '@tanstack/react-router'
import { InvoiceReview } from '@/features/invoice-review'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'
export const Route = createFileRoute('/_authenticated/invoice-review/')({ beforeLoad: () => { if ((useAuthStore.getState().auth.user?.role ?? 0) < ROLE.ADMIN) throw redirect({ to: '/403' }) }, component: InvoiceReview })
