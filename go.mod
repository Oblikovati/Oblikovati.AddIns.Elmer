// The oblikovati-elmer add-in: a c-shared library (.so/.dll/.dylib) loaded by the
// host at runtime, integrating the Elmer multiphysics FEM solver. It pulls a body's
// surface mesh + materials + selected faces from the host over the Apache-2.0 API,
// volume-meshes with a vendored mesher (gmsh), writes the solver input (SIF) and a
// native solver mesh directly, solves with a vendored headless ElmerSolver, parses
// the VTU results, and renders the field back as client graphics. The SHIPPED
// library links only the Apache-2.0 contract (oblikovati.org/api); the GPL host
// module is TEST-SCOPE ONLY (go.work / CI siblings), never linked into the .so.
module oblikovati.org/elmer

go 1.24.0

require oblikovati.org/api v0.102.1
