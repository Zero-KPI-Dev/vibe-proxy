import { useState, useEffect } from "react"
import {
  useAliases,
  useCreateAlias,
  useDeleteAlias,
  useUpdateAliasDefaults,
} from "@/hooks/use-aliases"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { EmptyState } from "@/components/empty-state"
import { toast } from "sonner"
import { Network, Plus, Trash2, ArrowRight } from "lucide-react"

export function ModelRoutingPage() {
  const { data, isLoading } = useAliases()
  const createAlias = useCreateAlias()
  const deleteAlias = useDeleteAlias()
  const updateDefaults = useUpdateAliasDefaults()

  const [showCreate, setShowCreate] = useState(false)
  const [newAlias, setNewAlias] = useState("")
  const [newTarget, setNewTarget] = useState("")
  const [defaultModel, setDefaultModel] = useState("")
  const [allowRaw, setAllowRaw] = useState(false)

  const aliases = data?.aliases ?? {}
  const aliasEntries = Object.entries(aliases).map(([alias, target]) => ({ alias, target }))

  // sync local state when data loads
  useEffect(() => {
    if (data) {
      setDefaultModel(data.default_model ?? "")
      setAllowRaw(data.allow_raw ?? false)
    }
  }, [data])

  const handleCreate = async () => {
    if (!newAlias.trim() || !newTarget.trim()) return
    try {
      await createAlias.mutateAsync({ alias: newAlias.trim(), target: newTarget.trim() })
      setNewAlias("")
      setNewTarget("")
      setShowCreate(false)
      toast.success(`Alias "${newAlias}" created`)
    } catch (e) {
      toast.error(`Failed to create alias: ${e instanceof Error ? e.message : "Unknown error"}`)
    }
  }

  const handleDelete = async (alias: string) => {
    try {
      await deleteAlias.mutateAsync(alias)
      toast.success(`Alias "${alias}" deleted`)
    } catch (e) {
      toast.error(`Failed to delete alias`)
    }
  }

  const handleSaveDefaults = async () => {
    try {
      await updateDefaults.mutateAsync({ default_model: defaultModel, allow_raw: allowRaw })
      toast.success("Defaults updated")
    } catch (e) {
      toast.error(`Failed to update defaults`)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Model Routing</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Configure virtual model aliases and routing rules
          </p>
        </div>
        <Dialog open={showCreate} onOpenChange={setShowCreate}>
          <DialogTrigger asChild>
            <Button size="sm">
              <Plus className="h-4 w-4 mr-1" />
              Add Alias
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Add Alias</DialogTitle>
              <DialogDescription>
                Map a virtual model name to a provider/model combination.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="aliasName">Alias Name</Label>
                <Input
                  id="aliasName"
                  placeholder="vibe-chat"
                  value={newAlias}
                  onChange={(e) => setNewAlias(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="aliasTarget">Target (provider/model)</Label>
                <Input
                  id="aliasTarget"
                  placeholder="openai/gpt-4"
                  value={newTarget}
                  onChange={(e) => setNewTarget(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  Format: <code className="text-xs">provider_id/model_name</code>
                </p>
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setShowCreate(false)}>
                Cancel
              </Button>
              <Button onClick={handleCreate} disabled={!newAlias.trim() || !newTarget.trim()}>
                Create Alias
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Alias Mappings</CardTitle>
          </CardHeader>
          <CardContent className="p-0">
            {isLoading ? (
              <div className="p-4 space-y-2">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            ) : aliasEntries.length === 0 ? (
              <EmptyState
                title="No aliases"
                description="Create aliases to map virtual model names to provider models."
                icon={<Network className="h-8 w-8" />}
              />
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Alias</TableHead>
                    <TableHead>Target</TableHead>
                    <TableHead className="w-20"></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {aliasEntries.map(({ alias, target }) => (
                    <TableRow key={alias}>
                      <TableCell className="font-mono text-sm font-medium">{alias}</TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Badge variant="outline" className="font-mono text-xs">{target}</Badge>
                        </div>
                      </TableCell>
                      <TableCell>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDelete(alias)}
                        >
                          <Trash2 className="h-3.5 w-3.5 text-destructive" />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Default Settings</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="defaultModel">Default Model</Label>
              <Input
                id="defaultModel"
                placeholder="gpt-4"
                value={defaultModel}
                onChange={(e) => setDefaultModel(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                Fallback model when none is specified in the request
              </p>
            </div>
            <div className="flex items-center justify-between">
              <div>
                <Label htmlFor="allowRaw">Allow Raw Models</Label>
                <p className="text-xs text-muted-foreground">
                  Allow clients to request models directly without an alias
                </p>
              </div>
              <Switch id="allowRaw" checked={allowRaw} onCheckedChange={setAllowRaw} />
            </div>
            <Button onClick={handleSaveDefaults} disabled={updateDefaults.isPending}>
              {updateDefaults.isPending ? "Saving..." : "Save Defaults"}
            </Button>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
