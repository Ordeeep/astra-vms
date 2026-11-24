using System;
using System.ComponentModel;
using System.Diagnostics;
using System.Runtime.InteropServices;

// The launcher holds the only job handle. Windows terminates its descendants
// even if the terminal disappears during startup or normal operation.
public static class DevProcessGroup
{
    [StructLayout(LayoutKind.Sequential)]
    private struct BasicLimits
    {
        public long ProcessTime, JobTime;
        public uint Flags;
        public UIntPtr MinWorkingSet, MaxWorkingSet;
        public uint ActiveProcesses;
        public UIntPtr Affinity;
        public uint PriorityClass, SchedulingClass;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct ExtendedLimits
    {
        public BasicLimits Basic;
        public ulong ReadOps, WriteOps, OtherOps, ReadBytes, WriteBytes, OtherBytes;
        public UIntPtr ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory;
    }

    [DllImport("kernel32", SetLastError = true)]
    private static extern IntPtr CreateJobObject(IntPtr attributes, string name);
    [DllImport("kernel32", SetLastError = true)]
    private static extern bool SetInformationJobObject(IntPtr job, int kind, ref ExtendedLimits limits, int length);
    [DllImport("kernel32", SetLastError = true)]
    private static extern bool AssignProcessToJobObject(IntPtr job, IntPtr process);
    [DllImport("kernel32", SetLastError = true)]
    private static extern bool SetConsoleCtrlHandler(ConsoleHandler handler, bool add);

    private delegate bool ConsoleHandler(uint signal);
    private static readonly ConsoleHandler Handler = OnConsoleSignal;
    private static string pgCtl, pgData;

    public static void Attach()
    {
        IntPtr job = CreateJobObject(IntPtr.Zero, null);
        ExtendedLimits limits = new ExtendedLimits();
        limits.Basic.Flags = 0x2000; // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
        if (job == IntPtr.Zero ||
            !SetInformationJobObject(job, 9, ref limits, Marshal.SizeOf(limits)) ||
            !AssignProcessToJobObject(job, Process.GetCurrentProcess().Handle))
            throw new Win32Exception();
        // Leave the raw handle open until the OS closes it at process exit.
    }

    public static void EnableDatabaseShutdown(string executable, string directory)
    {
        pgCtl = executable;
        pgData = directory;
        if (!SetConsoleCtrlHandler(Handler, true)) throw new Win32Exception();
    }

    private static bool OnConsoleSignal(uint signal)
    {
        if (signal == 2) // CTRL_CLOSE_EVENT: allow a fast database shutdown first.
        {
            try
            {
                var command = new ProcessStartInfo(pgCtl, "stop -D \"" + pgData + "\" -m fast -w -t 3");
                command.UseShellExecute = false;
                command.CreateNoWindow = true;
                using (var process = Process.Start(command)) process.WaitForExit(3500);
            }
            catch (SystemException) { } // The job still releases the entire process tree.
        }
        return false;
    }
}
